// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"context"
	"fmt"
	"time"

	"github.com/rinswind/deployment-operator/componentkit/controller"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
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
func (h *HelmOperations) Deploy(ctx context.Context) (*controller.ActionResult, error) {
	releaseName := h.config.ReleaseName
	log := logf.FromContext(ctx).WithValues("releaseName", releaseName)

	// Locate chart from configured source (settings are baked into chartSource)
	chartPath, err := h.chartSource.LocateChart(ctx)
	if err != nil {
		return controller.ActionResultForError(
			h.status,
			fmt.Errorf("failed to locate chart: %w", err),
			controller.AlwaysRetryOnError)
	}

	log.V(1).Info("Chart located", "path", chartPath)

	// Load chart from path
	chart, err := loader.Load(chartPath)
	if err != nil {
		return controller.ActionResultForError(
			h.status,
			fmt.Errorf("failed to load chart: %w", err),
			controller.AlwaysRetryOnError)
	}

	log.V(1).Info("Chart loaded", "name", chart.Metadata.Name, "version", chart.Metadata.Version)

	// Validate chart dependencies are pre-packaged
	if len(chart.Metadata.Dependencies) > 0 {
		log.V(1).Info("Validating chart dependencies", "count", len(chart.Metadata.Dependencies))

		if err := action.CheckDependencies(chart, chart.Metadata.Dependencies); err != nil {
			return controller.ActionResultForError(
				h.status,
				fmt.Errorf("chart has unfulfilled dependencies - charts must be pre-packaged with all dependencies included: %w", err),
				controller.AlwaysRetryOnError)
		}

		log.V(1).Info("Chart dependencies validated successfully")
	}

	// Check if release already exists
	getAction := action.NewGet(h.actionConfig)
	getAction.Version = 0 // Get latest version
	if rel, err := getAction.Run(releaseName); err == nil && rel != nil {
		log.Info("Release already exists, upgrading", "version", rel.Version)

		// Populate status with release name before upgrade (handles takeover of existing releases)
		if h.status.ReleaseName == "" {
			h.status.ReleaseName = rel.Name
		}

		return h.upgrade(ctx, chart)
	}

	log.Info("Release does not exist, installing new release")
	return h.install(ctx, chart)
}

func (h *HelmOperations) install(ctx context.Context, chart *chart.Chart) (*controller.ActionResult, error) {
	// Use the spec release name for installs
	releaseName := h.config.ReleaseName
	releaseNamespace := h.config.ReleaseNamespace

	log := logf.FromContext(ctx).WithValues("releaseName", releaseName, "namespace", releaseNamespace)

	// Create install action
	installAction := action.NewInstall(h.actionConfig)
	installAction.ReleaseName = releaseName
	installAction.Namespace = releaseNamespace
	installAction.CreateNamespace = *h.config.ManageNamespace
	installAction.Version = h.chartSource.GetVersion()
	installAction.Wait = false               // Async deployment - don't block reconcile loop
	installAction.Timeout = 30 * time.Second // Quick timeout for install operation itself

	// Use config values directly - already in correct nested format for Helm
	vals := h.config.Values

	// Install the chart
	rel, err := installAction.Run(chart, vals)
	if err != nil {
		return controller.ActionResultForError(
			h.status,
			fmt.Errorf("failed to install helm release %s: %w", releaseName, err),
			controller.AlwaysRetryOnError)
	}

	log.Info("Successfully installed helm release",
		"version", rel.Version,
		"status", rel.Info.Status.String())

	// Update status with new release information
	h.status.ReleaseVersion = rel.Version
	h.status.ReleaseName = rel.Name
	h.status.ChartVersion = rel.Chart.Metadata.Version
	h.status.LastDeployTime = time.Now().Format(time.RFC3339)

	details := fmt.Sprintf("Installing chart %s:%s", chart.Metadata.Name, chart.Metadata.Version)
	return controller.ActionSuccessWithDetails(h.status, details)
}

// startHelmReleaseUpgrade handles upgrading an existing Helm release using pre-parsed configuration
func (h *HelmOperations) upgrade(ctx context.Context, chart *chart.Chart) (*controller.ActionResult, error) {
	// Use status release name for upgrades
	releaseName := h.status.ReleaseName
	releaseNamespace := h.config.ReleaseNamespace

	log := logf.FromContext(ctx).WithValues("releaseName", releaseName, "namespace", releaseNamespace)

	// Create upgrade action
	upgradeAction := action.NewUpgrade(h.actionConfig)
	upgradeAction.Version = h.chartSource.GetVersion()
	upgradeAction.Wait = false               // Async upgrade - don't block reconcile loop
	upgradeAction.Timeout = 30 * time.Second // Quick timeout for upgrade operation itself

	// Use config values directly - already in correct nested format for Helm
	vals := h.config.Values

	// Upgrade the chart
	rel, err := upgradeAction.Run(releaseName, chart, vals)
	if err != nil {
		return controller.ActionResultForError(
			h.status,
			fmt.Errorf("failed to upgrade helm release %s: %w", releaseName, err),
			controller.AlwaysRetryOnError)
	}

	log.Info("Successfully started helm release upgrade",
		"version", rel.Version,
		"status", rel.Info.Status.String())

	// Update status with upgraded release information
	h.status.ReleaseVersion = rel.Version
	h.status.ReleaseName = rel.Name
	h.status.ChartVersion = rel.Chart.Metadata.Version
	h.status.LastDeployTime = time.Now().Format(time.RFC3339)

	details := fmt.Sprintf("Upgrading chart %s:%s", chart.Metadata.Name, chart.Metadata.Version)
	return controller.ActionSuccessWithDetails(h.status, details)
}

// checkReleaseDeployed verifies if a Helm release and all its resources are ready using pre-parsed configuration
// Returns OperationResult with Success indicating readiness status
func (h *HelmOperations) CheckDeployment(ctx context.Context) (*controller.CheckResult, error) {
	// Get the current release
	rel, err := h.getHelmRelease(h.status.ReleaseName)
	if err != nil {
		return controller.CheckResultForError(
			h.status,
			fmt.Errorf("failed to check helm release readiness: %w", err),
			controller.AlwaysRetryOnError)
	}

	// Build ResourceList from release manifest for non-blocking status checking
	resourceList, err := h.gatherHelmReleaseResources(ctx, rel)
	if err != nil {
		return controller.CheckResultForError(
			h.status,
			fmt.Errorf("failed to build resource list from release: %w", err),
			controller.AlwaysRetryOnError)
	}

	// Use non-blocking readiness check
	notReadyCount, err := h.checkResourcesReady(ctx, resourceList)
	if err != nil {
		return controller.CheckResultForError(
			h.status,
			fmt.Errorf("failed to check resources ready: %w", err),
			controller.AlwaysRetryOnError)
	}

	if notReadyCount == 0 {
		details := fmt.Sprintf("Release %s ready with %d resources", h.status.ReleaseName, len(resourceList))
		return controller.CheckCompleteWithDetails(h.status, details)
	}

	details := fmt.Sprintf("Waiting for %d of %d resources to be ready", notReadyCount, len(resourceList))
	return controller.CheckInProgressWithDetails(h.status, details)
}

// checkResourcesReady performs non-blocking readiness checks on Kubernetes resources.
// Returns the count of not-ready resources (0 means all ready) and any error encountered.
func (h *HelmOperations) checkResourcesReady(ctx context.Context, resourceList kube.ResourceList) (int, error) {
	log := logf.FromContext(ctx).WithValues("releaseName", h.status.ReleaseName)

	if len(resourceList) == 0 {
		log.Info("No resources to check, treating as ready")
		return 0, nil
	}

	// Create Kubernetes client for ReadyChecker using action config's REST client
	restConfig, err := h.actionConfig.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return 0, fmt.Errorf("failed to get REST config: %w", err)
	}

	kubernetesClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return 0, fmt.Errorf("failed to create kubernetes client: %w", err)
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

			return 0, fmt.Errorf("failed to check readiness of %s/%s: %w",
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

	if notReadyCount == 0 {
		log.Info("All resources ready", "total", len(resourceList))
	} else {
		log.Info("Some resources not ready", "notReady", notReadyCount, "total", len(resourceList))
	}

	return notReadyCount, nil
}
