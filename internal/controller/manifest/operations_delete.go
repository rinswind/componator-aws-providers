// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"context"
	"fmt"

	"github.com/rinswind/deployment-operator/componentkit/controller"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Delete initiates cleanup by deleting all applied resources in reverse order.
func (m *ManifestOperations) Delete(ctx context.Context) (*controller.OperationResult, error) {
	log := logf.FromContext(ctx).WithValues("appliedCount", len(m.status.AppliedResources))
	log.Info("Deleting manifests")

	// If no resources were applied, consider it complete
	if len(m.status.AppliedResources) == 0 {
		log.Info("No applied resources to delete")
		return m.successResult(), nil
	}

	// Delete resources in reverse order (helps with dependencies)
	for i := len(m.status.AppliedResources) - 1; i >= 0; i-- {
		ref := m.status.AppliedResources[i]
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
			// Log warning but continue with best-effort cleanup
			log.Error(err, "Failed to parse apiVersion, skipping resource")
			continue
		}

		gvk := schema.GroupVersionKind{
			Group:   gv.Group,
			Version: gv.Version,
			Kind:    ref.Kind,
		}

		// Get GVR from GVK
		mapping, err := m.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			// Log warning but continue with best-effort cleanup
			log.Error(err, "Failed to get REST mapping, skipping resource")
			continue
		}

		gvr := mapping.Resource

		// Determine if resource is namespaced
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			if ref.Namespace == "" {
				log.Error(fmt.Errorf("missing namespace"), "Namespaced resource missing namespace in reference, skipping")
				continue
			}
			err = m.dynamicClient.Resource(gvr).Namespace(ref.Namespace).Delete(ctx, ref.Name, deleteOptions())
		} else {
			err = m.dynamicClient.Resource(gvr).Delete(ctx, ref.Name, deleteOptions())
		}

		if err != nil {
			if errors.IsNotFound(err) {
				// Resource already deleted - this is fine
				log.V(1).Info("Resource already deleted")
			} else {
				// Log warning but continue with best-effort cleanup
				log.Error(err, "Failed to delete resource, continuing")
			}
		} else {
			log.Info("Successfully deleted resource")
		}
	}

	log.Info("Deletion initiated for all resources")

	// Return success immediately - deletion is complete
	// (Kubernetes handles finalizers and cascading deletion)
	return m.successResult(), nil
}

// CheckDeletion verifies that all resources have been deleted.
// Since we use foreground deletion and don't set finalizers on applied resources,
// this should complete immediately after Delete() returns.
func (m *ManifestOperations) CheckDeletion(ctx context.Context) (*controller.OperationResult, error) {
	log := logf.FromContext(ctx).WithValues("appliedCount", len(m.status.AppliedResources))
	log.V(1).Info("Checking deletion status")

	// If no resources were applied, consider it complete
	if len(m.status.AppliedResources) == 0 {
		log.Info("No applied resources, deletion complete")
		return m.successResult(), nil
	}

	// Check if any resources still exist
	anyExist := false
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
			// If we can't parse, skip this resource
			log.V(1).Info("Failed to parse apiVersion, skipping check")
			continue
		}

		gvk := schema.GroupVersionKind{
			Group:   gv.Group,
			Version: gv.Version,
			Kind:    ref.Kind,
		}

		// Get GVR from GVK
		mapping, err := m.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			// If we can't get mapping, skip this resource
			log.V(1).Info("Failed to get REST mapping, skipping check")
			continue
		}

		gvr := mapping.Resource

		// Check if resource still exists
		var err2 error
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			if ref.Namespace == "" {
				continue
			}
			_, err2 = m.dynamicClient.Resource(gvr).Namespace(ref.Namespace).Get(ctx, ref.Name, getOptions())
		} else {
			_, err2 = m.dynamicClient.Resource(gvr).Get(ctx, ref.Name, getOptions())
		}

		if err2 != nil {
			if errors.IsNotFound(err2) {
				// Resource deleted - this is what we want
				log.V(1).Info("Resource deleted")
			} else {
				// Error checking - log but continue
				log.Error(err2, "Failed to check resource status")
			}
		} else {
			// Resource still exists
			log.Info("Resource still exists")
			anyExist = true
		}
	}

	if anyExist {
		log.Info("Some resources still exist, waiting for deletion")
		return m.pendingResult(), nil
	}

	log.Info("All resources deleted")
	return m.successResult(), nil
}
