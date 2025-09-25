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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/go-playground/validator/v10"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/repo"
	"k8s.io/client-go/kubernetes"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

const (
	// DeploymentNamespaceAnnotation stores the actual namespace where Helm release was deployed
	DeploymentNamespaceAnnotation = "helm.deployment-orchestrator.io/target-namespace"
	// DeploymentReleaseNameAnnotation stores the actual release name used for Helm deployment
	DeploymentReleaseNameAnnotation = "helm.deployment-orchestrator.io/release-name"
)

// parseHelmConfig unmarshals Component.Spec.Config into HelmConfig struct
func parseHelmConfig(component *deploymentsv1alpha1.Component) (*HelmConfig, error) {
	if component.Spec.Config == nil {
		return nil, fmt.Errorf("config is required for helm components")
	}

	var config HelmConfig
	if err := json.Unmarshal(component.Spec.Config.Raw, &config); err != nil {
		return nil, fmt.Errorf("failed to parse helm config: %w", err)
	}

	// Validate configuration using validator framework
	validate := validator.New()
	if err := validate.Struct(&config); err != nil {
		return nil, fmt.Errorf("helm config validation failed: %w", err)
	}

	return &config, nil
}

// generateReleaseName creates a deterministic release name from Component metadata
func generateReleaseName(component *deploymentsv1alpha1.Component) string {
	// Use component name and namespace to ensure uniqueness
	return fmt.Sprintf("%s-%s", component.Namespace, component.Name)
}

// setupHelmActionConfig creates and initializes Helm settings and action configuration
// This is a common pattern used across multiple Helm operations
func setupHelmActionConfig(ctx context.Context, namespace string) (*cli.EnvSettings, *action.Configuration, error) {
	log := logf.FromContext(ctx)

	settings := cli.New()
	actionConfig := &action.Configuration{}

	// Initialize the action configuration with Kubernetes client
	if err := actionConfig.Init(settings.RESTClientGetter(), namespace, "secrets", func(format string, v ...any) {
		log.Info(fmt.Sprintf(format, v...))
	}); err != nil {
		return nil, nil, fmt.Errorf("failed to initialize helm action configuration: %w", err)
	}

	return settings, actionConfig, nil
}

// performHelmDeployment handles all Helm-specific deployment operations
// Returns a map of annotations that should be set on the Component
func performHelmDeployment(ctx context.Context, component *deploymentsv1alpha1.Component) (map[string]string, error) {
	log := logf.FromContext(ctx)

	// Parse the Helm configuration from Component.Spec.Config
	config, err := parseHelmConfig(component)
	if err != nil {
		return nil, fmt.Errorf("failed to parse helm configuration: %w", err)
	}

	// Generate deterministic release name
	releaseName := generateReleaseName(component)

	// Determine target namespace (use config.Namespace if specified, otherwise component namespace)
	targetNamespace := component.Namespace
	if config.Namespace != "" {
		targetNamespace = config.Namespace
	}

	log.Info("Parsed helm configuration",
		"repository", config.Repository.URL,
		"chart", config.Chart.Name,
		"version", config.Chart.Version,
		"releaseName", releaseName,
		"targetNamespace", targetNamespace,
		"valuesCount", len(config.Values))

	// Initialize Helm settings and action configuration
	settings, actionConfig, err := setupHelmActionConfig(ctx, targetNamespace)
	if err != nil {
		return nil, err
	}

	// Check if release already exists
	getAction := action.NewGet(actionConfig)
	getAction.Version = 0 // Get latest version
	if rel, err := getAction.Run(releaseName); err == nil && rel != nil {
		log.Info("Release already exists, skipping installation", "releaseName", releaseName, "version", rel.Version)
		// Return the same annotations that would be set during installation
		annotations := map[string]string{
			DeploymentNamespaceAnnotation:   targetNamespace,
			DeploymentReleaseNameAnnotation: releaseName,
		}
		return annotations, nil
	}

	// Create install action
	installAction := action.NewInstall(actionConfig)
	installAction.ReleaseName = releaseName
	installAction.Namespace = targetNamespace
	installAction.CreateNamespace = true
	installAction.Version = config.Chart.Version
	installAction.Wait = false               // Async deployment - don't block reconcile loop
	installAction.Timeout = 30 * time.Second // Quick timeout for install operation itself

	// Set up repository configuration properly for ephemeral containers
	// This creates temporary repository files that Helm can use normally
	if err := setupHelmRepository(settings, config.Repository.Name, config.Repository.URL); err != nil {
		return nil, fmt.Errorf("failed to setup helm repository: %w", err)
	}

	// Use Helm's standard chart resolution with repo/chart format
	// Now that repository is properly configured, this will work as expected
	chartRef := fmt.Sprintf("%s/%s", config.Repository.Name, config.Chart.Name)
	cp, err := installAction.ChartPathOptions.LocateChart(chartRef, settings)
	if err != nil {
		return nil, fmt.Errorf("failed to locate chart %s: %w", chartRef, err)
	}

	chart, err := loader.Load(cp)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart from %s: %w", cp, err)
	}

	// Convert config values to map[string]any
	vals := make(map[string]any)
	for key, value := range config.Values {
		vals[key] = value
	}

	// Install the chart
	rel, err := installAction.Run(chart, vals)
	if err != nil {
		return nil, fmt.Errorf("failed to install helm release %s: %w", releaseName, err)
	}

	log.Info("Successfully installed helm release",
		"releaseName", releaseName,
		"namespace", targetNamespace,
		"version", rel.Version,
		"status", rel.Info.Status.String())

	// Return annotations that should be set on the Component
	annotations := map[string]string{
		DeploymentNamespaceAnnotation:   targetNamespace,
		DeploymentReleaseNameAnnotation: releaseName,
	}

	return annotations, nil
}

// setupHelmRepository configures a Helm repository properly for ephemeral containers
// This creates the necessary repository configuration files that Helm expects
func setupHelmRepository(settings *cli.EnvSettings, repoName, repoURL string) error {
	// Create temporary directories for Helm configuration
	tempConfigDir, err := os.MkdirTemp("", "helm-config-")
	if err != nil {
		return fmt.Errorf("failed to create temporary config directory: %w", err)
	}

	tempCacheDir, err := os.MkdirTemp("", "helm-cache-")
	if err != nil {
		os.RemoveAll(tempConfigDir)
		return fmt.Errorf("failed to create temporary cache directory: %w", err)
	}

	// Configure Helm settings to use our temporary directories
	settings.RepositoryConfig = tempConfigDir + "/repositories.yaml"
	settings.RepositoryCache = tempCacheDir

	// Load or create repository file
	repoFile := repo.NewFile()

	// Create repository entry
	repoEntry := &repo.Entry{
		Name: repoName,
		URL:  repoURL,
	}

	// Create chart repository instance for index download
	chartRepo, err := repo.NewChartRepository(repoEntry, getter.All(settings))
	if err != nil {
		return fmt.Errorf("failed to create chart repository: %w", err)
	}

	// Set the cache path
	chartRepo.CachePath = settings.RepositoryCache

	// Download the repository index - this validates the repo and caches the index
	_, err = chartRepo.DownloadIndexFile()
	if err != nil {
		return fmt.Errorf("failed to download repository index: %w", err)
	}

	// Add repository to the configuration file
	repoFile.Update(repoEntry)

	// Write the repository configuration file
	if err := repoFile.WriteFile(settings.RepositoryConfig, 0644); err != nil {
		return fmt.Errorf("failed to write repository configuration: %w", err)
	}

	// Note: We don't clean up tempConfigDir and tempCacheDir here because
	// Helm will need them for the duration of the chart operations
	// The calling code should handle cleanup if needed, or rely on OS cleanup

	return nil
}

// performHelmCleanup handles all Helm-specific cleanup operations
func performHelmCleanup(ctx context.Context, component *deploymentsv1alpha1.Component) error {
	log := logf.FromContext(ctx)

	// Get release name from stored annotation
	releaseName := component.Annotations[DeploymentReleaseNameAnnotation]
	if releaseName == "" {
		return fmt.Errorf("release name annotation %s not found - component may not have been properly deployed", DeploymentReleaseNameAnnotation)
	}

	// Get target namespace from stored annotation
	targetNamespace := component.Annotations[DeploymentNamespaceAnnotation]
	if targetNamespace == "" {
		return fmt.Errorf("target namespace annotation %s not found - component may not have been properly deployed", DeploymentNamespaceAnnotation)
	}

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

// checkHelmReleaseReadiness verifies if a Helm release and its resources are ready
func getHelmRelease(ctx context.Context, component *deploymentsv1alpha1.Component) (*release.Release, error) {
	// Get release name from stored annotation
	releaseName := component.Annotations[DeploymentReleaseNameAnnotation]
	if releaseName == "" {
		return nil, fmt.Errorf("release name annotation %s not found", DeploymentReleaseNameAnnotation)
	}

	// Get target namespace from stored annotation
	targetNamespace := component.Annotations[DeploymentNamespaceAnnotation]
	if targetNamespace == "" {
		return nil, fmt.Errorf("target namespace annotation %s not found", DeploymentNamespaceAnnotation)
	}

	// Initialize Helm settings and action configuration
	_, actionConfig, err := setupHelmActionConfig(ctx, targetNamespace)
	if err != nil {
		return nil, err
	}

	// Get release status
	statusAction := action.NewStatus(actionConfig)
	rel, err := statusAction.Run(releaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to get release status: %w", err)
	}

	return rel, nil
}

// gatherHelmReleaseResources extracts Kubernetes resources from a Helm release manifest
// and builds a ResourceList for status checking
func gatherHelmReleaseResources(ctx context.Context, rel *release.Release) (kube.ResourceList, error) {
	log := logf.FromContext(ctx)

	if rel.Manifest == "" {
		log.Info("Release has no manifest, treating as ready")
		return kube.ResourceList{}, nil
	}

	// Initialize Helm settings and action configuration to get access to kube.Client
	_, actionConfig, err := setupHelmActionConfig(ctx, rel.Namespace)
	if err != nil {
		return nil, err
	}

	// Get the KubeClient from the action configuration
	kubeClient := actionConfig.KubeClient

	// Use Helm's Build function to parse the manifest into ResourceList
	resourceList, err := kubeClient.Build(bytes.NewBufferString(rel.Manifest), false)
	if err != nil {
		return nil, fmt.Errorf("failed to build resource list from manifest: %w", err)
	}

	log.Info("Built resource list from release manifest",
		"releaseName", rel.Name,
		"resourceCount", len(resourceList))

	return resourceList, nil
}

// checkHelmReleaseState performs non-blocking readiness checks on Kubernetes resources
func checkHelmReleaseState(ctx context.Context, resourceList kube.ResourceList) (bool, error) {
	log := logf.FromContext(ctx)

	if len(resourceList) == 0 {
		log.Info("No resources to check, treating as ready")
		return true, nil
	}

	// Initialize Helm settings and get REST client getter for ReadyChecker
	settings := cli.New()
	restClientGetter := settings.RESTClientGetter()

	// Create Kubernetes client for ReadyChecker
	restConfig, err := restClientGetter.ToRESTConfig()
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
