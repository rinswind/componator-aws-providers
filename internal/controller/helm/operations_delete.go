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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Delete starts asynchronous helm release deletion using pre-parsed configuration
func (h *HelmOperations) Delete(ctx context.Context) error {
	log := logf.FromContext(ctx)

	// Use pre-parsed configuration from factory (no repeated parsing)
	config := h.config

	releaseName := config.ReleaseName
	targetNamespace := config.ReleaseNamespace

	log.Info("Performing helm cleanup using pre-parsed configuration",
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

	// Clean up namespace if we're managing it
	if *config.ManageNamespace {
		if err := h.deleteNamespace(ctx, targetNamespace, actionConfig); err != nil {
			log.Error(err, "Failed to delete managed namespace", "namespace", targetNamespace)
			// Continue - don't fail Component deletion over namespace cleanup
		}
	}

	return nil
}

// checkDeletion verifies if a Helm release and all its resources have been deleted using pre-parsed configuration
// Returns (deleted, ioError, deletionError) to distinguish between temporary I/O issues and permanent failures
func (h *HelmOperations) CheckDeletion(ctx context.Context, elapsed time.Duration) (bool, error, error) {
	log := logf.FromContext(ctx)

	// Use pre-parsed configuration from factory (no repeated parsing)
	config := h.config

	// Check deletion timeout first
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
	rel, err := getHelmRelease(ctx, config.ReleaseName, config.ReleaseNamespace)
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

	if !allDeleted {
		log.Info("Some release resources still exist, deletion in progress")
		return false, nil, nil // Still in progress
	}

	log.Info("All release resources deleted")

	// Are we managing the namespace too?
	if !*config.ManageNamespace {
		log.Info("Deletion complete")
		return true, nil, nil
	}

	// Setup action configuration for potential namespace operations
	_, actionConfig, err := setupHelmActionConfig(ctx, config.ReleaseNamespace)
	if err != nil {
		return false, fmt.Errorf("failed to setup helm action config: %w", err), nil // I/O error
	}

	exists, err := h.namespaceExists(ctx, config.ReleaseNamespace, actionConfig)
	if err != nil {
		return false, fmt.Errorf("failed to check namespace status: %w", err), nil
	}

	if exists {
		log.Info("Managed namespace still exists, deletion in progress")
		return false, nil, nil
	}

	log.Info("Deletion complete")
	return true, nil, nil
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
		exists, err := resourceExists(ctx, resource)
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

// resourceExists checks if a specific Kubernetes resource still exists in the cluster
// Returns true if the resource exists, false if it's been deleted
func resourceExists(ctx context.Context, resource *resource.Info) (bool, error) {
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

func (h *HelmOperations) namespaceExists(
	ctx context.Context, namespace string, actionConfig *action.Configuration) (bool, error) {

	restConfig, err := actionConfig.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return false, err
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return false, err
	}

	_, err = clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err == nil {
		// Managed to get the namespace - so still here
		return true, nil
	}

	if apierrors.IsNotFound(err) {
		return false, nil
	}

	return false, err
}

func (h *HelmOperations) deleteNamespace(
	ctx context.Context, namespace string, actionConfig *action.Configuration) error {

	log := logf.FromContext(ctx)

	// Get Kubernetes client using the same approach as in deploy operations
	restConfig, err := actionConfig.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("failed to get REST config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Delete namespace - Kubernetes will handle cascade deletion and blocking until empty
	err = clientset.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete namespace: %w", err)
	}

	log.Info("Successfully initiated namespace deletion", "namespace", namespace)
	return nil
}
