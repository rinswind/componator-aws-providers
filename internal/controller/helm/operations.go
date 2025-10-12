// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/rinswind/deployment-operator-handlers/internal/controller/helm/source"
	"github.com/rinswind/deployment-operator-handlers/internal/controller/helm/source/http"
	"github.com/rinswind/deployment-operator-handlers/internal/controller/helm/source/oci"
	"github.com/rinswind/deployment-operator/componentkit/controller"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/release"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// HelmOperationsFactory implements the ComponentOperationsFactory interface for Helm deployments.
type HelmOperationsFactory struct {
	httpChartSource *http.CachingRepository // Singleton for HTTP chart operations with caching
	k8sClient       client.Client           // Kubernetes client for OCI credential resolution
}

// NewHelmOperationsFactory creates a new HelmOperationsFactory with the provided dependencies.
// The HTTP chart source is a singleton shared across all reconciliations.
// The k8sClient is used for OCI credential resolution from the namespace specified in the secret reference.
func NewHelmOperationsFactory(httpChartSource *http.CachingRepository, k8sClient client.Client) *HelmOperationsFactory {
	return &HelmOperationsFactory{
		httpChartSource: httpChartSource,
		k8sClient:       k8sClient,
	}
}

func (f *HelmOperationsFactory) NewOperations(
	ctx context.Context, rawConfig json.RawMessage, currentStatus json.RawMessage) (controller.ComponentOperations, error) {

	log := logf.FromContext(ctx)

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

	logFunc := func(format string, v ...any) {
		log.Info(fmt.Sprintf(format, v...))
	}

	// Initialize the action configuration with Kubernetes client
	if err := actionConfig.Init(settings.RESTClientGetter(), config.ReleaseNamespace, "secrets", logFunc); err != nil {
		return nil, fmt.Errorf("failed to initialize helm action configuration: %w", err)
	}

	// Create appropriate chart source based on configuration type
	var chartSource source.ChartSource

	switch src := config.Source.(type) {
	case *HTTPSource:
		// HTTP source: wrap singleton with per-chart configuration
		chartSource = http.NewChartSource(
			f.httpChartSource,
			src.Repository.Name,
			src.Repository.URL,
			src.Chart.Name,
			src.Chart.Version,
		)
		log.V(1).Info("Using HTTP chart source",
			"repository", src.Repository.URL,
			"chart", src.Chart.Name,
			"version", src.Chart.Version)

	case *OCISource:
		// OCI source: create new instance with authentication
		var secretRef *oci.SecretRef
		if src.Authentication != nil {
			secretRef = &oci.SecretRef{
				Name:      src.Authentication.SecretRef.Name,
				Namespace: src.Authentication.SecretRef.Namespace,
			}
		}

		chartSource = oci.NewChartSource(
			src.Chart,
			secretRef,
			f.k8sClient,
			actionConfig,
		)
		log.V(1).Info("Using OCI chart source",
			"chart", src.Chart,
			"hasAuth", src.Authentication != nil)

	default:
		return nil, fmt.Errorf("unsupported chart source type: %T", src)
	}

	return &HelmOperations{
		settings:     settings,
		actionConfig: actionConfig,
		config:       config,
		status:       status,
		chartSource:  chartSource,
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
	chartSource  source.ChartSource // Polymorphic chart source (HTTP or OCI)
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

// getChart retrieves the chart from the configured source.
// The source is already configured with all addressing parameters at construction time,
// so we just call GetChart with context and settings.
func (h *HelmOperations) getChart(ctx context.Context) (*chart.Chart, error) {
	return h.chartSource.GetChart(ctx, h.settings)
} // getChartVersion returns the chart version from the configured source.
// Temporary helper until Phase 3 interface simplification.
func (h *HelmOperations) getChartVersion() string {
	if httpSource := h.config.GetHTTPSource(); httpSource != nil {
		return httpSource.Chart.Version
	}
	if ociSource := h.config.GetOCISource(); ociSource != nil {
		// OCI references embed the version, extract it
		// For now, return empty (will be implemented in Phase 2)
		return ""
	}
	return ""
}
