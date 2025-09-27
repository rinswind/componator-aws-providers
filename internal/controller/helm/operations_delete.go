/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package helm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/storage/driver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/cli-runtime/pkg/resource"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

// startHelmReleaseDeletion handles all Helm-specific cleanup operations
func startHelmReleaseDeletion(ctx context.Context, component *deploymentsv1alpha1.Component) error {
	log := logf.FromContext(ctx)

	// Parse configuration to get release name and namespace
	config, err := resolveHelmConfig(component)
	if err != nil {
		return fmt.Errorf("failed to parse helm config: %w", err)
	}

	releaseName := config.ReleaseName
	targetNamespace := config.ReleaseNamespace

	log.Info("Performing helm cleanup",
		"releaseName", releaseName,
		"targetNamespace", targetNamespace)

	// Initialize Helm settings and action configuration
	_, actionConfig, err := setupHelmActionConfig(ctx, targetNamespace)
	if err != nil {
		return err
	}

	// Check if release exists
	getAction := action.NewGet(actionConfig)
	getAction.Version = 0 // Get latest version
	if _, err := getAction.Run(releaseName); err != nil {
		// Release doesn't exist, which is fine for cleanup
		log.Info("Release not found, cleanup already complete", "releaseName", releaseName)
		return nil
	}

	// Create uninstall action
	uninstallAction := action.NewUninstall(actionConfig)
	uninstallAction.Wait = false               // Async uninstall - don't block reconcile loop
	uninstallAction.Timeout = 30 * time.Second // Quick timeout for uninstall operation itself

	// Uninstall the release
	res, err := uninstallAction.Run(releaseName)
	if err != nil {
		return fmt.Errorf("failed to uninstall helm release %s: %w", releaseName, err)
	}

	log.Info("Successfully uninstalled helm release",
		"releaseName", releaseName,
		"namespace", targetNamespace,
		"info", res.Info)

	return nil
}

// checkHelmReleaseDeleted verifies if a Helm release and all its resources have been deleted
// Returns (deleted, ioError, deletionError) to distinguish between temporary I/O issues and permanent failures
func checkHelmReleaseDeleted(
	ctx context.Context, component *deploymentsv1alpha1.Component, elapsed time.Duration) (bool, error, error) {

	log := logf.FromContext(ctx)

	// Check deletion timeout first
	config, err := resolveHelmConfig(component)
	if err != nil {
		return false, fmt.Errorf("failed to resolve component config for timeout check: %w", err), nil // I/O error
	}

	deletionTimeout := config.Timeouts.Deletion.Duration
	if elapsed >= deletionTimeout {
		log.Error(nil, "deletion timed out",
			"elapsed", elapsed,
			"timeout", deletionTimeout,
			"chart", config.Chart.Name)

		return false, nil, fmt.Errorf("Deletion timed out after %v (timeout: %v)",
			elapsed.Truncate(time.Second), deletionTimeout) // Deletion failure
	}

	// Try to get the current release
	rel, err := getHelmRelease(ctx, component)
	if err != nil {
		// If release is gone, deletion is complete
		if errors.Is(err, driver.ErrReleaseNotFound) {
			log.Info("Release no longer exists, deletion complete")
			return true, nil, nil // Success
		}
		return false, fmt.Errorf("failed to check release status during deletion: %w", err), nil // I/O error
	}

	// Release still exists - check if its resources are gone
	log.Info("Release still exists, checking if resources are deleted",
		"releaseName", rel.Name,
		"status", rel.Info.Status.String())

	resourceList, err := gatherHelmReleaseResources(ctx, rel)
	if err != nil {
		return false, fmt.Errorf("failed to gather resources for deletion check: %w", err), nil // I/O error
	}

	// Check if all resources from manifest are deleted
	allDeleted, err := checkResourcesDeleted(ctx, resourceList)
	if err != nil {
		return false, fmt.Errorf("failed to check resource deletion status: %w", err), nil // I/O error
	}

	if allDeleted {
		log.Info("All release resources deleted, deletion complete")
	} else {
		log.Info("Some release resources still exist, deletion in progress")
	}

	return allDeleted, nil, nil // Success or still in progress
}

// checkResourcesDeleted performs non-blocking deletion checks on Kubernetes resources
// Returns true when all resources from the list no longer exist in the cluster
func checkResourcesDeleted(ctx context.Context, resourceList kube.ResourceList) (bool, error) {
	log := logf.FromContext(ctx)

	if len(resourceList) == 0 {
		log.Info("No resources to check for deletion, treating as deleted")
		return true, nil
	}

	stillExistCount := 0

	// Check each resource individually (non-blocking)
	// Use the resource's own REST client - no need for separate Kubernetes client setup
	for _, resource := range resourceList {
		exists, err := resourceStillExists(ctx, resource)
		if err != nil {
			log.Error(err, "Error checking resource existence during deletion",
				"resource", resource.Name,
				"kind", resource.Mapping.GroupVersionKind.Kind,
				"namespace", resource.Namespace)
			return false, fmt.Errorf("failed to check existence of %s/%s: %w",
				resource.Mapping.GroupVersionKind.Kind, resource.Name, err)
		}

		if exists {
			stillExistCount++
			log.V(1).Info("Resource still exists",
				"resource", resource.Name,
				"kind", resource.Mapping.GroupVersionKind.Kind,
				"namespace", resource.Namespace)
		}
	}

	allDeleted := (stillExistCount == 0)
	if !allDeleted {
		log.Info("Some resources still exist", "stillExist", stillExistCount, "total", len(resourceList))
	} else {
		log.Info("All resources deleted", "total", len(resourceList))
	}

	return allDeleted, nil
}

// resourceStillExists checks if a specific Kubernetes resource still exists in the cluster
// Returns true if the resource exists, false if it's been deleted
func resourceStillExists(ctx context.Context, resource *resource.Info) (bool, error) {
	// Use the resource's REST client with proper verb/path construction
	result := resource.Client.Get().
		Resource(resource.Mapping.Resource.Resource).
		Namespace(resource.Namespace).
		Name(resource.Name).
		Do(ctx)

	if err := result.Error(); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil // Resource deleted
		}
		return false, err // Other error
	}

	return true, nil // Resource exists
}
