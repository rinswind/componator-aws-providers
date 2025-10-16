// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"context"
	"fmt"

	"github.com/rinswind/deployment-operator/componentkit/controller"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

		// Get properly scoped resource interface for this manifest
		gvk := obj.GroupVersionKind()
		resourceClient, err := m.getResourceInterfaceForGVK(gvk, obj.GetNamespace())
		if err != nil {
			log.Error(err, "Failed to resolve resource")
			return m.errorResult(ctx, err)
		}

		// Apply using server-side apply
		log.Info("Applying manifest")
		_, err = resourceClient.Apply(ctx, obj.GetName(), obj, applyOptions(fieldManager))
		if err != nil {
			err = fmt.Errorf("failed to apply manifest %s %s/%s: %w", gvk.String(), obj.GetNamespace(), obj.GetName(), err)
			log.Error(err, "Apply failed")
			return m.errorResult(ctx, err)
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
	return m.pendingResult(ctx)
}

// CheckDeployment verifies the readiness of all applied resources using kstatus.
func (m *ManifestOperations) CheckDeployment(ctx context.Context) (*controller.OperationResult, error) {
	log := logf.FromContext(ctx).WithValues("appliedCount", len(m.status.AppliedResources))
	log.V(1).Info("Checking deployment status")

	// If no resources were applied, consider it ready (edge case)
	if len(m.status.AppliedResources) == 0 {
		log.Info("No applied resources to check, considering ready")
		return m.successResult(ctx)
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

		// Get properly scoped resource interface
		resourceClient, err := m.getResourceInterface(ref)
		if err != nil {
			log.Error(err, "Failed to resolve resource")
			return m.errorResult(ctx, err)
		}

		// Get current resource from API server
		obj, err := resourceClient.Get(ctx, ref.Name, getOptions())
		if err != nil {
			err = fmt.Errorf("resource disappeared: %w", err)
			log.Error(err, "Resource not found")
			return m.errorResult(ctx, err)
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
			err := resourceErrorf(ref, "failed: %s", statusResult.Message)
			log.Error(err, "Resource failed")
			return m.errorResult(ctx, err)

		case status.TerminatingStatus:
			// Resource is being deleted (unexpected during deployment)
			err := resourceErrorf(ref, "is terminating")
			log.Error(err, "Resource terminating")
			return m.errorResult(ctx, err)

		case status.NotFoundStatus:
			// Resource disappeared
			err := resourceErrorf(ref, "not found")
			log.Error(err, "Resource disappeared")
			return m.errorResult(ctx, err)

		default:
			// Unknown status
			log.Info("Resource has unknown status", "status", statusResult.Status)
			allReady = false
		}
	}

	if allReady {
		log.Info("All resources are ready")
		return m.successResult(ctx)
	}

	log.Info("Some resources not ready yet")
	return m.pendingResult(ctx)
}
