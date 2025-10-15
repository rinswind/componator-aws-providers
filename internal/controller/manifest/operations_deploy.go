// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"context"
	"fmt"

	"github.com/rinswind/deployment-operator/componentkit/controller"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// fieldManager identifies this controller as the owner of applied fields
	fieldManager = "manifest-handler"
	// trackingLabel is added to all applied resources for identification
	trackingLabelKey = "manifest.deployment-orchestrator.io/component"
)

// Deploy initiates the deployment by applying all manifests to the cluster.
func (m *ManifestOperations) Deploy(ctx context.Context) (*controller.OperationResult, error) {
	log := logf.FromContext(ctx).WithValues("manifestCount", len(m.config.Manifests))
	log.Info("Deploying manifests")

	// Clear previous applied resources list (if re-deploying)
	m.status.AppliedResources = make([]ResourceReference, 0, len(m.config.Manifests))

	// Apply each manifest
	for i, manifestMap := range m.config.Manifests {
		log := log.WithValues("manifestIndex", i)

		// Convert map to unstructured object
		obj := &unstructured.Unstructured{Object: manifestMap}

		// Validate required fields
		if obj.GetAPIVersion() == "" || obj.GetKind() == "" || obj.GetName() == "" {
			err := fmt.Errorf("manifest at index %d missing required fields (apiVersion, kind, or name)", i)
			log.Error(err, "Invalid manifest")
			return m.errorResult(err), nil
		}

		log = log.WithValues(
			"apiVersion", obj.GetAPIVersion(),
			"kind", obj.GetKind(),
			"name", obj.GetName(),
			"namespace", obj.GetNamespace(),
		)

		// Add tracking label to identify resources managed by this component
		labels := obj.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		// Note: Component name would need to be passed in context or config
		// For now, we'll use a generic label
		labels[trackingLabelKey] = "true"
		obj.SetLabels(labels)

		// Get GVR from GVK using RESTMapper
		gvk := obj.GroupVersionKind()
		mapping, err := m.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			err = fmt.Errorf("failed to get REST mapping for %s: %w", gvk.String(), err)
			log.Error(err, "REST mapping failed")
			return m.errorResult(err), nil
		}

		gvr := mapping.Resource
		log.V(1).Info("Resolved GVR", "gvr", gvr.String())

		// Determine if resource is namespaced
		var resourceClient dynamic.ResourceInterface
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			// Namespaced resource
			namespace := obj.GetNamespace()
			if namespace == "" {
				err := fmt.Errorf("namespaced resource %s missing namespace", gvk.String())
				log.Error(err, "Missing namespace")
				return m.errorResult(err), nil
			}
			resourceClient = m.dynamicClient.Resource(gvr).Namespace(namespace)
		} else {
			// Cluster-scoped resource
			resourceClient = m.dynamicClient.Resource(gvr)
		}

		// Apply using server-side apply
		log.Info("Applying manifest")
		_, err = resourceClient.Apply(ctx, obj.GetName(), obj, applyOptions(fieldManager))
		if err != nil {
			err = fmt.Errorf("failed to apply manifest %s %s/%s: %w", gvk.String(), obj.GetNamespace(), obj.GetName(), err)
			log.Error(err, "Apply failed")
			return m.errorResult(err), nil
		}

		// Record applied resource reference
		m.status.AppliedResources = append(m.status.AppliedResources, ResourceReference{
			APIVersion: obj.GetAPIVersion(),
			Kind:       obj.GetKind(),
			Name:       obj.GetName(),
			Namespace:  obj.GetNamespace(),
		})

		log.Info("Successfully applied manifest")
	}

	log.Info("All manifests applied successfully", "appliedCount", len(m.status.AppliedResources))

	// Return pending - need to check status separately
	return m.pendingResult(), nil
}

// CheckDeployment verifies the readiness of all applied resources using kstatus.
func (m *ManifestOperations) CheckDeployment(ctx context.Context) (*controller.OperationResult, error) {
	log := logf.FromContext(ctx).WithValues("appliedCount", len(m.status.AppliedResources))
	log.V(1).Info("Checking deployment status")

	// If no resources were applied, consider it ready (edge case)
	if len(m.status.AppliedResources) == 0 {
		log.Info("No applied resources to check, considering ready")
		return m.successResult(), nil
	}

	// Check status of each applied resource
	allReady := true
	for i, ref := range m.status.AppliedResources {
		log := log.WithValues(
			"resourceIndex", i,
			"apiVersion", ref.APIVersion,
			"kind", ref.Kind,
			"name", ref.Name,
			"namespace", ref.Namespace,
		)

		// Parse GVK from reference
		gv, err := schema.ParseGroupVersion(ref.APIVersion)
		if err != nil {
			err = fmt.Errorf("invalid apiVersion %s: %w", ref.APIVersion, err)
			log.Error(err, "Failed to parse apiVersion")
			return m.errorResult(err), nil
		}

		gvk := schema.GroupVersionKind{
			Group:   gv.Group,
			Version: gv.Version,
			Kind:    ref.Kind,
		}

		// Get GVR from GVK
		mapping, err := m.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			err = fmt.Errorf("failed to get REST mapping for %s: %w", gvk.String(), err)
			log.Error(err, "REST mapping failed")
			return m.errorResult(err), nil
		}

		gvr := mapping.Resource

		// Get current resource from API server
		var resourceClient dynamic.ResourceInterface
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			if ref.Namespace == "" {
				err := fmt.Errorf("namespaced resource %s missing namespace in reference", gvk.String())
				log.Error(err, "Missing namespace in reference")
				return m.errorResult(err), nil
			}
			resourceClient = m.dynamicClient.Resource(gvr).Namespace(ref.Namespace)
		} else {
			resourceClient = m.dynamicClient.Resource(gvr)
		}

		obj, err := resourceClient.Get(ctx, ref.Name, getOptions())
		if err != nil {
			err = fmt.Errorf("resource %s %s/%s disappeared: %w", gvk.String(), ref.Namespace, ref.Name, err)
			log.Error(err, "Resource not found")
			return m.errorResult(err), nil
		}

		// Compute status using kstatus
		statusResult, err := status.Compute(obj)
		if err != nil {
			log.Error(err, "Failed to compute status")
			// If we can't compute status, keep checking
			allReady = false
			continue
		}

		log.V(1).Info("Resource status", "status", statusResult.Status, "message", statusResult.Message)

		// Map kstatus result to our readiness state
		switch statusResult.Status {
		case status.CurrentStatus:
			// Resource is ready
			log.V(1).Info("Resource is ready")
			continue

		case status.InProgressStatus, status.UnknownStatus:
			// Still progressing or unknown - not ready yet
			log.Info("Resource not ready yet", "status", statusResult.Status, "message", statusResult.Message)
			allReady = false

		case status.FailedStatus:
			// Resource has failed
			err := fmt.Errorf("resource %s %s/%s failed: %s", gvk.String(), ref.Namespace, ref.Name, statusResult.Message)
			log.Error(err, "Resource failed")
			return m.errorResult(err), nil

		case status.TerminatingStatus:
			// Resource is being deleted (unexpected during deployment)
			err := fmt.Errorf("resource %s %s/%s is terminating", gvk.String(), ref.Namespace, ref.Name)
			log.Error(err, "Resource terminating")
			return m.errorResult(err), nil

		case status.NotFoundStatus:
			// Resource disappeared
			err := fmt.Errorf("resource %s %s/%s not found", gvk.String(), ref.Namespace, ref.Name)
			log.Error(err, "Resource disappeared")
			return m.errorResult(err), nil

		default:
			// Unknown status
			log.Info("Resource has unknown status", "status", statusResult.Status)
			allReady = false
		}
	}

	if allReady {
		log.Info("All resources are ready")
		return m.successResult(), nil
	}

	log.Info("Some resources not ready yet")
	return m.pendingResult(), nil
}
