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
	"fmt"
	"time"

	"github.com/rinswind/deployment-operator/componentkit/controller"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/kube"
	"k8s.io/client-go/kubernetes"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Deploy handles all Helm-specific deployment operations using pre-parsed configuration
// Implements ComponentOperations.Deploy interface method.
//
// For initial deployments, uses config.ReleaseName from the Component spec.
// After successful deployment, persists the actual release name to status so that
// all subsequent operations use the deployed name for consistency.
func (h *HelmOperations) Deploy(ctx context.Context) (*controller.OperationResult, error) {
	log := logf.FromContext(ctx)

	releaseName := h.config.ReleaseName

	// Set up repository configuration properly for ephemeral containers
	if _, err := setupHelmRepository(h.config, h.settings); err != nil {
		return nil, fmt.Errorf("failed to setup helm repository: %w", err)
	}

	// Prepare chart for installation
	chart, err := loadHelmChart(h.config, h.settings)
	if err != nil {
		return nil, err
	}

	// Check if release already exists
	getAction := action.NewGet(h.actionConfig)
	getAction.Version = 0 // Get latest version
	if rel, err := getAction.Run(releaseName); err == nil && rel != nil {
		log.Info("Release already exists, upgrading", "releaseName", releaseName, "version", rel.Version)

		return h.upgrade(ctx, chart)
	}

	log.Info("Release does not exist, installing new release", "releaseName", releaseName)
	return h.install(ctx, chart)
}

func (h *HelmOperations) install(ctx context.Context, chart *chart.Chart) (*controller.OperationResult, error) {
	log := logf.FromContext(ctx)

	// Use the spec release name for installs
	releaseName := h.config.ReleaseName
	releaseNamespace := h.config.ReleaseNamespace

	// Create install action
	installAction := action.NewInstall(h.actionConfig)
	installAction.ReleaseName = releaseName
	installAction.Namespace = releaseNamespace
	installAction.CreateNamespace = *h.config.ManageNamespace
	installAction.Version = h.config.Chart.Version
	installAction.Wait = false               // Async deployment - don't block reconcile loop
	installAction.Timeout = 30 * time.Second // Quick timeout for install operation itself

	// Use config values directly - already in correct nested format for Helm
	vals := h.config.Values

	// Install the chart
	rel, err := installAction.Run(chart, vals)
	if err != nil {
		return nil, fmt.Errorf("failed to install helm release %s: %w", releaseName, err)
	}

	log.Info("Successfully installed helm release",
		"releaseName", releaseName,
		"namespace", releaseNamespace,
		"version", rel.Version,
		"status", rel.Info.Status.String())

	// Update status with new release information
	h.status.ReleaseVersion = rel.Version
	h.status.ReleaseName = rel.Name
	h.status.ChartVersion = rel.Chart.Metadata.Version
	h.status.LastDeployTime = time.Now().Format(time.RFC3339)

	return h.successResult(), nil
}

// startHelmReleaseUpgrade handles upgrading an existing Helm release using pre-parsed configuration
func (h *HelmOperations) upgrade(ctx context.Context, chart *chart.Chart) (*controller.OperationResult, error) {
	log := logf.FromContext(ctx)

	// Use status release name for upgrades
	releaseName := h.status.ReleaseName
	releaseNamespace := h.config.ReleaseNamespace

	// Create upgrade action
	upgradeAction := action.NewUpgrade(h.actionConfig)
	upgradeAction.Version = h.config.Chart.Version
	upgradeAction.Wait = false               // Async upgrade - don't block reconcile loop
	upgradeAction.Timeout = 30 * time.Second // Quick timeout for upgrade operation itself

	// Use config values directly - already in correct nested format for Helm
	vals := h.config.Values

	// Upgrade the chart
	rel, err := upgradeAction.Run(releaseName, chart, vals)
	if err != nil {
		return nil, fmt.Errorf("failed to upgrade helm release %s: %w", releaseName, err)
	}

	log.Info("Successfully started helm release upgrade",
		"releaseName", releaseName,
		"namespace", releaseNamespace,
		"version", rel.Version,
		"status", rel.Info.Status.String())

	// Update status with upgraded release information
	h.status.ReleaseVersion = rel.Version
	h.status.ReleaseName = rel.Name
	h.status.ChartVersion = rel.Chart.Metadata.Version
	h.status.LastDeployTime = time.Now().Format(time.RFC3339)

	return h.successResult(), nil
}

// checkReleaseDeployed verifies if a Helm release and all its resources are ready using pre-parsed configuration
// Returns OperationResult with Success indicating readiness status
func (h *HelmOperations) CheckDeployment(ctx context.Context) (*controller.OperationResult, error) {
	// Get the current release
	rel, err := h.getHelmRelease(h.status.ReleaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to check helm release readiness: %w", err) // I/O error
	}

	// Build ResourceList from release manifest for non-blocking status checking
	resourceList, err := h.gatherHelmReleaseResources(ctx, rel)
	if err != nil {
		return nil, fmt.Errorf("failed to build resource list from release: %w", err) // I/O error
	}

	// Use non-blocking readiness check
	ready, err := h.checkResourcesReady(ctx, resourceList)
	if err != nil {
		return h.errorResult(fmt.Errorf("deployment failed: %w", err)), nil
	}

	if ready {
		return h.successResult(), nil
	}
	return h.pendingResult(), nil
}

// checkResourcesReady performs non-blocking readiness checks on Kubernetes resources
func (h *HelmOperations) checkResourcesReady(ctx context.Context, resourceList kube.ResourceList) (bool, error) {
	log := logf.FromContext(ctx)

	if len(resourceList) == 0 {
		log.Info("No resources to check, treating as ready")
		return true, nil
	}

	// Create Kubernetes client for ReadyChecker using action config's REST client
	restConfig, err := h.actionConfig.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return false, fmt.Errorf("failed to get REST config: %w", err)
	}

	kubernetesClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return false, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Create ReadyChecker - this is Helm's non-blocking readiness checker
	readyChecker := kube.NewReadyChecker(kubernetesClient, func(format string, v ...any) {
		log.V(1).Info(fmt.Sprintf(format, v...))
	})

	notReadyCount := 0

	// Check each resource individually (non-blocking)
	for _, resource := range resourceList {
		ready, err := readyChecker.IsReady(ctx, resource)

		if err != nil {
			log.Error(err, "Error checking resource readiness",
				"resource", resource.Name,
				"kind", resource.Mapping.GroupVersionKind.Kind,
				"namespace", resource.Namespace)

			return false, fmt.Errorf("failed to check readiness of %s/%s: %w",
				resource.Mapping.GroupVersionKind.Kind, resource.Name, err)
		}

		if !ready {
			notReadyCount++
			log.V(1).Info("Resource not ready",
				"resource", resource.Name,
				"kind", resource.Mapping.GroupVersionKind.Kind,
				"namespace", resource.Namespace)
		}
	}

	allReady := (notReadyCount == 0)
	if allReady {
		log.Info("All resources ready", "total", len(resourceList))
	} else {
		log.Info("Some resources not ready", "notReady", notReadyCount, "total", len(resourceList))
	}

	return allReady, nil
}
