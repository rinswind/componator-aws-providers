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

	"github.com/rinswind/deployment-operator/handler/base"
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
func (h *HelmOperations) Delete(ctx context.Context) (*base.OperationResult, error) {
	log := logf.FromContext(ctx)

	releaseName := h.config.ReleaseName
	targetNamespace := h.config.ReleaseNamespace

	log.Info("Performing helm cleanup using pre-parsed configuration",
		"releaseName", releaseName,
		"targetNamespace", targetNamespace)

	// Check if release exists
	getAction := action.NewGet(h.actionConfig)
	getAction.Version = 0 // Get latest version
	if _, err := getAction.Run(releaseName); err != nil {
		// Release doesn't exist, which is fine for cleanup
		log.Info("Release not found, cleanup already complete", "releaseName", releaseName)

		return h.successResult(), nil
	}

	// Create uninstall action
	uninstallAction := action.NewUninstall(h.actionConfig)
	uninstallAction.Wait = false               // Async uninstall - don't block reconcile loop
	uninstallAction.Timeout = 30 * time.Second // Quick timeout for uninstall operation itself

	// Uninstall the release
	res, err := uninstallAction.Run(releaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to uninstall helm release %s: %w", releaseName, err)
	}

	log.Info("Successfully uninstalled helm release",
		"releaseName", releaseName,
		"namespace", targetNamespace,
		"info", res.Info)

	// Clean up namespace if we're managing it
	if *h.config.ManageNamespace {
		if err := h.deleteNamespace(ctx); err != nil {
			log.Error(err, "Failed to delete managed namespace", "namespace", targetNamespace)
			// Continue - don't fail Component deletion over namespace cleanup
		}
	}

	return h.successResult(), nil
}

// checkDeletion verifies if a Helm release and all its resources have been deleted using pre-parsed configuration
// Returns OperationResult with Success indicating deletion completion status
func (h *HelmOperations) CheckDeletion(ctx context.Context, elapsed time.Duration) (*base.OperationResult, error) {
	log := logf.FromContext(ctx)

	// Check deletion timeout first
	deletionTimeout := h.config.Timeouts.Deletion.Duration
	if elapsed >= deletionTimeout {
		log.Error(nil, "deletion timed out",
			"elapsed", elapsed,
			"timeout", deletionTimeout,
			"chart", h.config.Chart.Name)

		return h.errorResult(fmt.Errorf("Deletion timed out after %v (timeout: %v)", elapsed.Truncate(time.Second), deletionTimeout)), nil
	}

	// Try to get the current release
	rel, err := h.getHelmRelease(ctx)
	if err != nil {
		// If release is gone, deletion is complete
		if errors.Is(err, driver.ErrReleaseNotFound) {
			log.Info("Release no longer exists, deletion complete")
			return h.successResult(), nil
		}
		return nil, fmt.Errorf("failed to check release status during deletion: %w", err) // I/O error
	}

	// Release still exists - check if its resources are gone
	log.Info("Release still exists, checking if resources are deleted",
		"releaseName", rel.Name,
		"status", rel.Info.Status.String())

	resourceList, err := h.gatherHelmReleaseResources(ctx, rel)
	if err != nil {
		return nil, fmt.Errorf("failed to gather resources for deletion check: %w", err) // I/O error
	}

	// Check if all resources from manifest are deleted
	allDeleted, err := checkResourcesDeleted(ctx, resourceList)
	if err != nil {
		return nil, fmt.Errorf("failed to check resource deletion status: %w", err) // I/O error
	}

	if !allDeleted {
		log.Info("Some release resources still exist, deletion in progress")
		return h.pendingResult(), nil
	}

	log.Info("All release resources deleted")

	// Are we managing the namespace too?
	if !*h.config.ManageNamespace {
		log.Info("Deletion complete")
		return h.successResult(), nil
	}

	exists, err := h.namespaceExists(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check namespace status: %w", err)
	}

	if exists {
		log.Info("Managed namespace still exists, deletion in progress")
		return h.pendingResult(), nil
	}

	log.Info("Deletion complete")
	return h.successResult(), nil
}

// checkResourcesDeleted performs non-blocking deletion checks on Kubernetes resources
// Returns true when all resources from the list no longer exist in the cluster
func checkResourcesDeleted(ctx context.Context, resourceList kube.ResourceList) (bool, error) {
	log := logf.FromContext(ctx)

	if len(resourceList) == 0 {
		log.Info("No resources to check for deletion, treating as deleted")
		return true, nil
	}

	existsCount := 0

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
			existsCount++
			log.V(1).Info("Resource still exists",
				"resource", resource.Name,
				"kind", resource.Mapping.GroupVersionKind.Kind,
				"namespace", resource.Namespace)
		}
	}

	allDeleted := (existsCount == 0)
	if !allDeleted {
		log.Info("Some resources still exist", "stillExist", existsCount, "total", len(resourceList))
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

func (h *HelmOperations) namespaceExists(ctx context.Context) (bool, error) {
	restConfig, err := h.actionConfig.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return false, err
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return false, err
	}

	_, err = clientset.CoreV1().Namespaces().Get(ctx, h.config.ReleaseName, metav1.GetOptions{})
	if err == nil {
		// Managed to get the namespace - so still here
		return true, nil
	}

	if apierrors.IsNotFound(err) {
		return false, nil
	}

	return false, err
}

func (h *HelmOperations) deleteNamespace(ctx context.Context) error {
	log := logf.FromContext(ctx)

	// Get Kubernetes client using the same approach as in deploy operations
	restConfig, err := h.actionConfig.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("failed to get REST config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Delete namespace - Kubernetes will handle cascade deletion and blocking until empty
	err = clientset.CoreV1().Namespaces().Delete(ctx, h.config.ReleaseName, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete namespace: %w", err)
	}

	log.Info("Successfully initiated namespace deletion", "namespace", h.config.ReleaseName)
	return nil
}
