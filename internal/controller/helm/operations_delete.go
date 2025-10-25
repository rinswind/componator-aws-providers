// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rinswind/componator/componentkit/controller"
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
func (h *HelmOperations) Delete(ctx context.Context) (*controller.ActionResult, error) {
	// Use effective release name from status for deletion
	releaseName := h.status.ReleaseName
	targetNamespace := h.config.ReleaseNamespace

	log := logf.FromContext(ctx).WithValues("releaseName", releaseName, "namespace", targetNamespace)
	log.Info("Performing helm cleanup using effective release name")

	// Check if release exists
	getAction := action.NewGet(h.actionConfig)
	getAction.Version = 0 // Get latest version
	if _, err := getAction.Run(releaseName); err != nil {
		// Release doesn't exist, which is fine for cleanup
		log.Info("Release not found, cleanup already complete", "releaseName", releaseName)

		return controller.ActionSuccess(h.status)
	}

	// Create uninstall action
	uninstallAction := action.NewUninstall(h.actionConfig)
	uninstallAction.Wait = false               // Async uninstall - don't block reconcile loop
	uninstallAction.Timeout = 30 * time.Second // Quick timeout for uninstall operation itself

	// Uninstall the release
	res, err := uninstallAction.Run(releaseName)
	if err != nil {
		return controller.ActionResultForError(
			h.status,
			fmt.Errorf("failed to uninstall helm release %s: %w", releaseName, err),
			controller.AlwaysRetryOnError)
	}

	log.Info("Successfully uninstalled helm release", "info", res.Info)

	// Clean up namespace if we're managing it
	if *h.config.ManageNamespace {
		if err := h.deleteNamespace(ctx); err != nil {
			log.Error(err, "Failed to delete managed namespace", "namespace", targetNamespace)
			// Continue - don't fail Component deletion over namespace cleanup
		}
	}

	details := fmt.Sprintf("Deleting release %s", releaseName)
	return controller.ActionSuccessWithDetails(h.status, details)
}

// checkDeletion verifies if a Helm release and all its resources have been deleted using pre-parsed configuration
// Returns OperationResult with Success indicating deletion completion status
func (h *HelmOperations) CheckDeletion(ctx context.Context) (*controller.CheckResult, error) {
	log := logf.FromContext(ctx).WithValues("releaseName", h.status.ReleaseName)

	// Try to get the current release
	rel, err := h.getHelmRelease(h.status.ReleaseName)
	if err != nil {
		// If release is gone, deletion is complete
		if errors.Is(err, driver.ErrReleaseNotFound) {
			log.Info("Release no longer exists, deletion complete")
			return controller.CheckComplete(h.status)
		}
		return controller.CheckResultForError(
			h.status,
			fmt.Errorf("failed to check release status during deletion: %w", err),
			controller.AlwaysRetryOnError)
	}

	// Release still exists - check if its resources are gone
	log.Info("Release still exists, checking if resources are deleted", "status", rel.Info.Status.String())

	resourceList, err := h.gatherHelmReleaseResources(ctx, rel)
	if err != nil {
		return controller.CheckResultForError(
			h.status,
			fmt.Errorf("failed to gather resources for deletion check: %w", err),
			controller.AlwaysRetryOnError)
	}

	// Check if all resources from manifest are deleted
	remainingCount, err := checkResourcesDeleted(ctx, resourceList)
	if err != nil {
		return controller.CheckResultForError(
			h.status,
			fmt.Errorf("failed to check resource deletion status: %w", err),
			controller.AlwaysRetryOnError)
	}

	if remainingCount > 0 {
		log.Info("Some release resources still exist, deletion in progress")
		details := fmt.Sprintf("Waiting for %d of %d resources to be deleted", remainingCount, len(resourceList))
		return controller.CheckInProgressWithDetails(h.status, details)
	}

	log.Info("All release resources deleted")

	// Are we managing the namespace too?
	if !*h.config.ManageNamespace {
		log.Info("Deletion complete")
		details := fmt.Sprintf("Release %s deleted", h.status.ReleaseName)
		return controller.CheckCompleteWithDetails(h.status, details)
	}

	exists, err := h.namespaceExists(ctx)
	if err != nil {
		return controller.CheckResultForError(
			h.status,
			fmt.Errorf("failed to check namespace status: %w", err),
			controller.AlwaysRetryOnError)
	}

	if exists {
		log.Info("Managed namespace still exists, deletion in progress")
		details := fmt.Sprintf("Waiting for namespace %s deletion", h.config.ReleaseNamespace)
		return controller.CheckInProgressWithDetails(h.status, details)
	}

	log.Info("Deletion complete")
	details := fmt.Sprintf("Release %s deleted", h.status.ReleaseName)
	return controller.CheckCompleteWithDetails(h.status, details)
}

// checkResourcesDeleted performs non-blocking deletion checks on Kubernetes resources.
// Returns the count of resources still existing (0 means all deleted) and any error encountered.
func checkResourcesDeleted(ctx context.Context, resourceList kube.ResourceList) (int, error) {
	log := logf.FromContext(ctx).WithValues("totalResources", len(resourceList))

	if len(resourceList) == 0 {
		log.Info("No resources to check for deletion, treating as deleted")
		return 0, nil
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
			return 0, fmt.Errorf("failed to check existence of %s/%s: %w",
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

	if existsCount == 0 {
		log.Info("All resources deleted", "total", len(resourceList))
	} else {
		log.Info("Some resources still exist", "stillExist", existsCount, "total", len(resourceList))
	}

	return existsCount, nil
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

	// Use the namespace from config - namespace typically doesn't change
	_, err = clientset.CoreV1().Namespaces().Get(ctx, h.config.ReleaseNamespace, metav1.GetOptions{})
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
	log := logf.FromContext(ctx).WithValues("namespace", h.config.ReleaseNamespace)

	// Get Kubernetes client using the same approach as in deploy operations
	restConfig, err := h.actionConfig.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("failed to get REST config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Delete namespace - use config namespace for deletion
	err = clientset.CoreV1().Namespaces().Delete(ctx, h.config.ReleaseNamespace, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete namespace: %w", err)
	}

	log.Info("Successfully initiated namespace deletion")
	return nil
}
