// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/rinswind/deployment-operator-handlers/internal/controller/helm/source/http"
	"github.com/rinswind/deployment-operator/componentkit/controller"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/release"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// HelmOperationsFactory implements the ComponentOperationsFactory interface for Helm deployments.
type HelmOperationsFactory struct {
	httpChartSource *http.ChartSource // Singleton for HTTP chart operations with caching
}

// NewHelmOperationsFactory creates a new HelmOperationsFactory with HTTPChartSource singleton.
// Configuration is controlled via environment variables:
//   - HELM_INDEX_CACHE_SIZE: Max number of indexes to cache (default: 10, 0 = disabled)
//   - HELM_INDEX_CACHE_TTL: Time-to-live for cached indexes (default: "1h")
func NewHelmOperationsFactory() (*HelmOperationsFactory, error) {
	cacheSize := getEnvOrDefaultInt("HELM_INDEX_CACHE_SIZE", 10)
	cacheTTL := getEnvOrDefaultDuration("HELM_INDEX_CACHE_TTL", 1*time.Hour)

	chartSource, err := http.NewChartSource("./helm", cacheSize, cacheTTL, 5*time.Minute)
	if err != nil {
		return nil, err
	}

	return &HelmOperationsFactory{
		httpChartSource: chartSource,
	}, nil
}

func (f *HelmOperationsFactory) NewOperations(
	ctx context.Context, rawConfig json.RawMessage, currentStatus json.RawMessage) (controller.ComponentOperations, error) {

	config, err := resolveHelmConfig(ctx, rawConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to parse helm configuration: %w", err)
	}

	status, err := resolveHelmStatus(ctx, currentStatus)
	if err != nil {
		return nil, fmt.Errorf("failed to parse helm configuration: %w", err)
	}

	settings := cli.New()
	actionConfig := &action.Configuration{}

	log := logf.FromContext(ctx)
	logFunc := func(format string, v ...any) {
		log.Info(fmt.Sprintf(format, v...))
	}

	// Initialize the action configuration with Kubernetes client
	if err := actionConfig.Init(settings.RESTClientGetter(), config.ReleaseNamespace, "secrets", logFunc); err != nil {
		return nil, fmt.Errorf("failed to initialize helm action configuration: %w", err)
	}

	return &HelmOperations{
		settings:     settings,
		actionConfig: actionConfig,
		config:       config,
		status:       status,
		chartSource:  f.httpChartSource,
	}, nil
}

// HelmOperations implements the ComponentOperations interface for Helm-based deployments.
// This struct provides all Helm-specific deployment, upgrade, and deletion operations
// with pre-parsed configuration maintained throughout the reconciliation loop.
type HelmOperations struct {
	config *HelmConfig
	status *HelmStatus

	settings     *cli.EnvSettings
	actionConfig *action.Configuration
	chartSource  *http.ChartSource // Reference to factory's singleton
}

// getHelmRelease verifies if a Helm release exists and returns it
func (h *HelmOperations) getHelmRelease(releaseName string) (*release.Release, error) {
	statusAction := action.NewStatus(h.actionConfig)

	rel, err := statusAction.Run(releaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to get release status: %w", err)
	}

	return rel, nil
}

// successResult creates an OperationResult for successful operations
func (h *HelmOperations) successResult() *controller.OperationResult {
	updatedStatus, _ := json.Marshal(h.status)
	return &controller.OperationResult{
		UpdatedStatus: updatedStatus,
		Success:       true,
	}
}

// errorResult creates an OperationResult for failed operations with error details
func (h *HelmOperations) errorResult(err error) *controller.OperationResult {
	updatedStatus, _ := json.Marshal(h.status)
	return &controller.OperationResult{
		UpdatedStatus:  updatedStatus,
		Success:        false,
		OperationError: err,
	}
}

// pendingResult creates an OperationResult for operations still in progress
func (h *HelmOperations) pendingResult() *controller.OperationResult {
	updatedStatus, _ := json.Marshal(h.status)
	return &controller.OperationResult{
		UpdatedStatus: updatedStatus,
		Success:       false,
	}
}

// gatherHelmReleaseResources extracts Kubernetes resources from a Helm release manifest
// and builds a ResourceList for status checking
func (h *HelmOperations) gatherHelmReleaseResources(ctx context.Context, rel *release.Release) (kube.ResourceList, error) {
	log := logf.FromContext(ctx)

	if rel.Manifest == "" {
		log.Info("Release has no manifest, treating as ready")
		return kube.ResourceList{}, nil
	}

	// Get the KubeClient from the action configuration
	kubeClient := h.actionConfig.KubeClient

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

// Environment variable helpers for configuration

func getEnvOrDefaultInt(key string, defaultValue int) int {
	if val := os.Getenv(key); val != "" {
		if parsed, err := fmt.Sscanf(val, "%d", &defaultValue); err == nil && parsed == 1 {
			return defaultValue
		}
	}
	return defaultValue
}

func getEnvOrDefaultDuration(key string, defaultValue time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if parsed, err := time.ParseDuration(val); err == nil {
			return parsed
		}
	}
	return defaultValue
}
