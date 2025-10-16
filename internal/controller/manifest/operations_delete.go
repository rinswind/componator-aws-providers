// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"context"

	"github.com/rinswind/deployment-operator/componentkit/controller"
	"k8s.io/apimachinery/pkg/api/errors"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Delete initiates cleanup by deleting all applied resources in reverse order.
func (m *ManifestOperations) Delete(ctx context.Context) (*controller.OperationResult, error) {
	log := logf.FromContext(ctx).WithValues("appliedCount", len(m.status.AppliedResources))
	log.Info("Deleting manifests")

	// If no resources were applied, consider it complete
	if len(m.status.AppliedResources) == 0 {
		log.Info("No applied resources to delete")
		return m.successResult(ctx)
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

		// Get properly scoped resource interface
		resourceClient, err := m.getResourceInterface(ref)
		if err != nil {
			// Log warning but continue with best-effort cleanup
			log.Error(err, "Failed to resolve resource, skipping")
			continue
		}

		// Delete the resource
		err = resourceClient.Delete(ctx, ref.Name, deleteOptions())
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
	return m.successResult(ctx)
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
		return m.successResult(ctx)
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

		// Get properly scoped resource interface
		resourceClient, err := m.getResourceInterface(ref)
		if err != nil {
			// If we can't resolve, skip this resource
			log.V(1).Info("Failed to resolve resource, skipping check")
			continue
		}

		// Check if resource still exists
		_, err = resourceClient.Get(ctx, ref.Name, getOptions())
		if err != nil {
			if errors.IsNotFound(err) {
				// Resource deleted - this is what we want
				log.V(1).Info("Resource deleted")
			} else {
				// Error checking - log but continue
				log.Error(err, "Failed to check resource status")
			}
		} else {
			// Resource still exists
			log.Info("Resource still exists")
			anyExist = true
		}
	}

	if anyExist {
		log.Info("Some resources still exist, waiting for deletion")
		return m.pendingResult(ctx)
	}

	log.Info("All resources deleted")
	return m.successResult(ctx)
}
