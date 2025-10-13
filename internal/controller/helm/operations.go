// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/rinswind/deployment-operator-handlers/internal/controller/helm/sources"
	"github.com/rinswind/deployment-operator/componentkit/controller"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/release"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// HelmOperationsFactory implements the ComponentOperationsFactory interface for Helm deployments.
type HelmOperationsFactory struct {
	sourceRegistry sources.Registry // Registry of chart source instances
}

// NewHelmOperationsFactory creates a new HelmOperationsFactory with the source registry.
// The registry contains pre-created source instances (HTTP, OCI) that are configured per reconciliation.
func NewHelmOperationsFactory(sourceRegistry sources.Registry) *HelmOperationsFactory {
	return &HelmOperationsFactory{
		sourceRegistry: sourceRegistry,
	}
}

func (f *HelmOperationsFactory) NewOperations(
	ctx context.Context, rawConfig json.RawMessage, currentStatus json.RawMessage) (controller.ComponentOperations, error) {

	log := logf.FromContext(ctx)

	// Step 1: Detect source type from config
	sourceType, err := sources.DetectSourceType(rawConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to detect source type: %w", err)
	}

	// Step 2: Retrieve source from registry
	chartSource, err := f.sourceRegistry.Get(sourceType)
	if err != nil {
		return nil, fmt.Errorf("failed to get chart source: %w", err)
	}

	// Step 3: Parse and validate source-specific configuration
	if err := chartSource.ParseAndValidate(ctx, rawConfig); err != nil {
		return nil, fmt.Errorf("failed to parse source configuration: %w", err)
	}

	log.V(1).Info("Configured chart source",
		"type", sourceType,
		"version", chartSource.GetVersion())

	// Step 4: Parse source-agnostic helm configuration
	config, err := resolveHelmConfig(ctx, rawConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to parse helm configuration: %w", err)
	}

	status, err := resolveHelmStatus(ctx, currentStatus)
	if err != nil {
		return nil, fmt.Errorf("failed to parse helm status: %w", err)
	}

	// Step 5: Initialize Helm action configuration
	settings := cli.New()
	actionConfig := &action.Configuration{}

	logFunc := func(format string, v ...any) {
		log.Info(fmt.Sprintf(format, v...))
	}

	if err := actionConfig.Init(settings.RESTClientGetter(), config.ReleaseNamespace, "secrets", logFunc); err != nil {
		return nil, fmt.Errorf("failed to initialize helm action configuration: %w", err)
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
	chartSource  sources.ChartSource // Polymorphic chart source (HTTP or OCI)
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
