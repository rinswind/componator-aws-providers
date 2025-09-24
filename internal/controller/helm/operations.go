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
	"os"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/repo"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

const (
	// DeploymentNamespaceAnnotation stores the actual namespace where Helm release was deployed
	DeploymentNamespaceAnnotation = "helm.deployment-orchestrator.io/target-namespace"
	// DeploymentReleaseNameAnnotation stores the actual release name used for Helm deployment
	DeploymentReleaseNameAnnotation = "helm.deployment-orchestrator.io/release-name"
)

// performHelmDeployment handles all Helm-specific deployment operations
// Returns a map of annotations that should be set on the Component
func (r *ComponentReconciler) performHelmDeployment(ctx context.Context, component *deploymentsv1alpha1.Component) (map[string]string, error) {
	log := logf.FromContext(ctx)

	// Parse the Helm configuration from Component.Spec.Config
	config, err := r.parseHelmConfig(component)
	if err != nil {
		return nil, fmt.Errorf("failed to parse helm configuration: %w", err)
	}

	// Generate deterministic release name
	releaseName := r.generateReleaseName(component)

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
	settings := cli.New()
	actionConfig := &action.Configuration{}

	// Initialize the action configuration with Kubernetes client
	if err := actionConfig.Init(settings.RESTClientGetter(), targetNamespace, "secrets", func(format string, v ...interface{}) {
		log.Info(fmt.Sprintf(format, v...))
	}); err != nil {
		return nil, fmt.Errorf("failed to initialize helm action configuration: %w", err)
	}

	// Check if release already exists
	getAction := action.NewGet(actionConfig)
	getAction.Version = 0 // Get latest version
	if rel, err := getAction.Run(releaseName); err == nil && rel != nil {
		log.Info("Release already exists, skipping installation", "releaseName", releaseName, "version", rel.Version)
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
	installAction.Wait = true

	// Set up repository configuration properly for ephemeral containers
	// This creates temporary repository files that Helm can use normally
	if err := r.setupHelmRepository(settings, config.Repository.Name, config.Repository.URL); err != nil {
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

	// Convert config values to map[string]interface{}
	vals := make(map[string]interface{})
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
func (r *ComponentReconciler) setupHelmRepository(settings *cli.EnvSettings, repoName, repoURL string) error {
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
func (r *ComponentReconciler) performHelmCleanup(ctx context.Context, component *deploymentsv1alpha1.Component) error {
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
	settings := cli.New()
	actionConfig := &action.Configuration{}

	// Initialize the action configuration with Kubernetes client
	if err := actionConfig.Init(settings.RESTClientGetter(), targetNamespace, "secrets", func(format string, v ...interface{}) {
		log.Info(fmt.Sprintf(format, v...))
	}); err != nil {
		return fmt.Errorf("failed to initialize helm action configuration: %w", err)
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
	uninstallAction.Wait = true

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
