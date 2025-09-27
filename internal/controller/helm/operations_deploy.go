/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
y	// Create install action
	installAction := action.NewInstall(actionConfig)
	installAction.ReleaseName = releaseName
	installAction.Namespace = targetNamespace
	installAction.CreateNamespace = config.GetManageNamespace()
	installAction.Version = config.Chart.Version
	installAction.Wait = false               // Async deployment - don't block reconcile loop
	installAction.Timeout = 30 * time.Second // Quick timeout for install operation itselfot use this file except in compliance with the License.
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
	"os"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/repo"
	"k8s.io/client-go/kubernetes"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

// Deploy handles all Helm-specific deployment operations
// Implements ComponentOperations.Deploy interface method.
func (h *HelmOperations) Deploy(ctx context.Context, component *deploymentsv1alpha1.Component) error {
	log := logf.FromContext(ctx)

	// Parse the Helm configuration from Component.Spec.Config
	config, err := resolveHelmConfig(component)
	if err != nil {
		return fmt.Errorf("failed to parse helm configuration: %w", err)
	}

	// Get release name and target namespace from resolved configuration
	releaseName := config.ReleaseName
	releaseNamespace := config.ReleaseNamespace

	log.Info("Parsed helm configuration",
		"repository", config.Repository.URL,
		"chart", config.Chart.Name,
		"version", config.Chart.Version,
		"releaseName", releaseName,
		"releaseNamespace", releaseNamespace,
		"valuesCount", len(config.Values))

	// Initialize Helm settings and action configuration
	settings, actionConfig, err := setupHelmActionConfig(ctx, releaseNamespace)
	if err != nil {
		return err
	}

	// Check if release already exists
	getAction := action.NewGet(actionConfig)
	getAction.Version = 0 // Get latest version
	if rel, err := getAction.Run(releaseName); err == nil && rel != nil {
		log.Info("Release already exists, skipping installation", "releaseName", releaseName, "version", rel.Version)
		return nil
	}

	// Set up repository configuration properly for ephemeral containers
	if _, err := setupHelmRepository(config, settings); err != nil {
		return fmt.Errorf("failed to setup helm repository: %w", err)
	}

	// Prepare chart for installation
	chart, err := loadHelmChart(config, settings)
	if err != nil {
		return err
	}

	// Create install action
	installAction := action.NewInstall(actionConfig)
	installAction.ReleaseName = releaseName
	installAction.Namespace = releaseNamespace
	installAction.CreateNamespace = *config.ManageNamespace
	installAction.Version = config.Chart.Version
	installAction.Wait = false               // Async deployment - don't block reconcile loop
	installAction.Timeout = 30 * time.Second // Quick timeout for install operation itself

	// Use config values directly - already in correct nested format for Helm
	vals := config.Values

	// Install the chart
	rel, err := installAction.Run(chart, vals)
	if err != nil {
		return fmt.Errorf("failed to install helm release %s: %w", releaseName, err)
	}

	log.Info("Successfully installed helm release",
		"releaseName", releaseName,
		"namespace", releaseNamespace,
		"version", rel.Version,
		"status", rel.Info.Status.String())

	return nil
}

// startHelmReleaseUpgrade handles upgrading an existing Helm release with new configuration
func (h *HelmOperations) Upgrade(ctx context.Context, component *deploymentsv1alpha1.Component) error {
	log := logf.FromContext(ctx)

	// Parse the Helm configuration from Component.Spec.Config
	config, err := resolveHelmConfig(component)
	if err != nil {
		return fmt.Errorf("failed to parse helm configuration: %w", err)
	}

	// Get release name and target namespace from resolved configuration
	releaseName := config.ReleaseName
	releaseNamespace := config.ReleaseNamespace

	log.Info("Parsed helm configuration for upgrade",
		"repository", config.Repository.URL,
		"chart", config.Chart.Name,
		"version", config.Chart.Version,
		"releaseName", releaseName,
		"releaseNamespace", releaseNamespace,
		"valuesCount", len(config.Values))

	// Initialize Helm settings and action configuration
	settings, actionConfig, err := setupHelmActionConfig(ctx, releaseNamespace)
	if err != nil {
		return err
	}

	// Verify release exists before attempting upgrade
	getAction := action.NewGet(actionConfig)
	if _, err := getAction.Run(releaseName); err != nil {
		return fmt.Errorf("release %s not found for upgrade: %w", releaseName, err)
	}

	// Set up repository configuration properly for ephemeral containers
	if _, err := setupHelmRepository(config, settings); err != nil {
		return fmt.Errorf("failed to setup helm repository: %w", err)
	}

	// Prepare chart for upgrade
	chart, err := loadHelmChart(config, settings)
	if err != nil {
		return err
	}

	// Create upgrade action
	upgradeAction := action.NewUpgrade(actionConfig)
	upgradeAction.Version = config.Chart.Version
	upgradeAction.Wait = false               // Async upgrade - don't block reconcile loop
	upgradeAction.Timeout = 30 * time.Second // Quick timeout for upgrade operation itself

	// Use config values directly - already in correct nested format for Helm
	vals := config.Values

	// Upgrade the chart
	rel, err := upgradeAction.Run(releaseName, chart, vals)
	if err != nil {
		return fmt.Errorf("failed to upgrade helm release %s: %w", releaseName, err)
	}

	log.Info("Successfully started helm release upgrade",
		"releaseName", releaseName,
		"namespace", releaseNamespace,
		"version", rel.Version,
		"status", rel.Info.Status.String())

	return nil
}

// loadHelmChart handles the common chart preparation steps for both install and upgrade
// Returns the loaded chart ready for deployment
func loadHelmChart(config *HelmConfig, settings *cli.EnvSettings) (*chart.Chart, error) {
	// Use Helm's standard chart resolution with repo/chart format
	chartRef := fmt.Sprintf("%s/%s", config.Repository.Name, config.Chart.Name)
	chartPathOptions := &action.ChartPathOptions{}

	cp, err := chartPathOptions.LocateChart(chartRef, settings)
	if err != nil {
		return nil, fmt.Errorf("failed to locate chart %s: %w", chartRef, err)
	}

	chart, err := loader.Load(cp)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart from %s: %w", cp, err)
	}

	return chart, nil
}

// setupHelmRepository configures a Helm repository properly for ephemeral containers
// This creates the necessary repository configuration files that Helm expects
func setupHelmRepository(config *HelmConfig, settings *cli.EnvSettings) (*repo.ChartRepository, error) {
	// Create temporary directories for Helm configuration
	tempConfigDir, err := os.MkdirTemp("", "helm-config-")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary config directory: %w", err)
	}

	tempCacheDir, err := os.MkdirTemp("", "helm-cache-")
	if err != nil {
		os.RemoveAll(tempConfigDir)
		return nil, fmt.Errorf("failed to create temporary cache directory: %w", err)
	}

	// Configure Helm settings to use our temporary directories
	settings.RepositoryConfig = tempConfigDir + "/repositories.yaml"
	settings.RepositoryCache = tempCacheDir

	// Load or create repository file
	repoFile := repo.NewFile()

	// Create repository entry
	repoEntry := &repo.Entry{
		Name: config.Repository.Name,
		URL:  config.Repository.URL,
	}

	// Create chart repository instance for index download
	chartRepo, err := repo.NewChartRepository(repoEntry, getter.All(settings))
	if err != nil {
		return nil, fmt.Errorf("failed to create chart repository: %w", err)
	}

	// Set the cache path
	chartRepo.CachePath = settings.RepositoryCache

	// Download the repository index - this validates the repo and caches the index
	_, err = chartRepo.DownloadIndexFile()
	if err != nil {
		return nil, fmt.Errorf("failed to download repository index: %w", err)
	}

	// Add repository to the configuration file
	repoFile.Update(repoEntry)

	// Write the repository configuration file
	if err := repoFile.WriteFile(settings.RepositoryConfig, 0644); err != nil {
		return nil, fmt.Errorf("failed to write repository configuration: %w", err)
	}

	// Note: We don't clean up tempConfigDir and tempCacheDir here because
	// Helm will need them for the duration of the chart operations
	// The calling code should handle cleanup if needed, or rely on OS cleanup

	return chartRepo, nil
}

// checkReleaseDeployed verifies if a Helm release and all its resources are ready
// Returns (ready, ioError, deploymentError) to distinguish between temporary I/O issues and permanent failures
func (h *HelmOperations) CheckDeployment(
	ctx context.Context, component *deploymentsv1alpha1.Component, elapsed time.Duration) (bool, error, error) {

	log := logf.FromContext(ctx)

	// Check deployment timeout first
	config, err := resolveHelmConfig(component)
	if err != nil {
		return false, fmt.Errorf("failed to resolve component config for timeout check: %w", err), nil // I/O error
	}

	deploymentTimeout := config.Timeouts.Deployment.Duration
	if elapsed >= deploymentTimeout {
		log.Error(nil, "deployment timed out",
			"elapsed", elapsed,
			"timeout", deploymentTimeout,
			"chart", config.Chart.Name)

		return false, nil, fmt.Errorf("Deployment timed out after %v (timeout: %v)",
			elapsed.Truncate(time.Second), deploymentTimeout) // Deployment failure
	}

	// Get the current release
	rel, err := getHelmRelease(ctx, component)
	if err != nil {
		return false, fmt.Errorf("failed to check helm release readiness: %w", err), nil // I/O error
	}

	// Build ResourceList from release manifest for non-blocking status checking
	resourceList, err := gatherHelmReleaseResources(ctx, rel)
	if err != nil {
		return false, fmt.Errorf("failed to build resource list from release: %w", err), nil // I/O error
	}

	// Use non-blocking readiness check
	ready, err := checkHelmReleaseReady(ctx, rel.Namespace, resourceList)
	if err != nil {
		return false, nil, fmt.Errorf("deployment failed: %w", err) // Deployment failure
	}

	if ready {
		log.Info("Deployment succeeded")
	} else {
		log.Info("Helm release resources not ready yet")
	}

	return ready, nil, nil
}

// checkHelmReleaseReady performs non-blocking readiness checks on Kubernetes resources
func checkHelmReleaseReady(ctx context.Context, namespace string, resourceList kube.ResourceList) (bool, error) {
	log := logf.FromContext(ctx)

	if len(resourceList) == 0 {
		log.Info("No resources to check, treating as ready")
		return true, nil
	}

	// Use setupHelmActionConfig for consistent infrastructure
	_, actionConfig, err := setupHelmActionConfig(ctx, namespace)
	if err != nil {
		return false, err
	}

	// Create Kubernetes client for ReadyChecker using action config's REST client
	restConfig, err := actionConfig.RESTClientGetter.ToRESTConfig()
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
	if !allReady {
		log.Info("Some resources not ready", "notReady", notReadyCount, "total", len(resourceList))
	} else {
		log.Info("All resources ready", "total", len(resourceList))
	}

	return allReady, nil
}
