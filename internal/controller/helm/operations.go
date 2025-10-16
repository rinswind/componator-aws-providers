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
	chartSourceFactory sources.ChartSourceFactory
}

// NewHelmOperationsFactory creates a new HelmOperationsFactory with the factory registry.
// The registry contains stateless factory instances that create sources per reconciliation.
func NewHelmOperationsFactory(sourceFactory sources.ChartSourceFactory) *HelmOperationsFactory {
	return &HelmOperationsFactory{
		chartSourceFactory: sourceFactory,
	}
}

func (f *HelmOperationsFactory) NewOperations(
	ctx context.Context, rawConfig json.RawMessage, rawStatus json.RawMessage) (controller.ComponentOperations, error) {

	// Step 0: Initialize settings (needed for both factory and actionConfig)
	settings := cli.New()

	// Step 1: Parse the helm config. Leaves the source part untouched
	config, err := resolveHelmConfig(ctx, rawConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to parse helm configuration: %w", err)
	}

	log := logf.FromContext(ctx).WithValues(
		"releaseName", config.ReleaseName,
		"namespace", config.ReleaseNamespace)

	// Step 2: Extract source section from config
	var configMap map[string]json.RawMessage
	if err := json.Unmarshal(rawConfig, &configMap); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	rawSource, hasSource := configMap["source"]
	if !hasSource {
		return nil, fmt.Errorf("source field is required in helm configuration")
	}

	// Step 3: Parse the source part of the config (pass just the source section)
	chartSource, err := f.chartSourceFactory.CreateSource(ctx, rawSource, settings)
	if err != nil {
		return nil, fmt.Errorf("failed to create chart source: %w", err)
	}

	log.V(1).Info("Created chart source", "version", chartSource.GetVersion())

	// Step 3: Parse the helm status
	status, err := resolveHelmStatus(ctx, rawStatus)
	if err != nil {
		return nil, fmt.Errorf("failed to parse helm status: %w", err)
	}

	// Step 4: Initialize Helm action configuration
	actionConfig := &action.Configuration{}

	logFunc := func(format string, v ...any) {
		log.Info(fmt.Sprintf(format, v...))
	}

	if err := actionConfig.Init(settings.RESTClientGetter(), config.ReleaseNamespace, "secrets", logFunc); err != nil {
		return nil, fmt.Errorf("failed to initialize helm action configuration: %w", err)
	}

	return &HelmOperations{
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

	actionConfig *action.Configuration
	chartSource  sources.ChartSource // Polymorphic chart source (HTTP or OCI) with settings baked in
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

// successResult creates an OperationResult for successful operations.
// Returns the result and nil error, matching the ComponentOperations method signatures.
func (h *HelmOperations) successResult() (*controller.OperationResult, error) {
	updatedStatus, _ := json.Marshal(h.status)
	return &controller.OperationResult{
		UpdatedStatus: updatedStatus,
		Success:       true,
	}, nil
}

// errorResult creates a standardized error response for Helm operations.
// Unlike RDS and Manifest handlers, Helm treats ALL errors as transient because the Helm SDK
// abstracts away error details making reliable classification impractical. Helm operations involve
// multiple layers (chart sources, file I/O, Helm actions, Kubernetes API) with errors wrapped by
// the SDK. Conservative approach: retry all errors rather than risk marking transient issues permanent.
func (h *HelmOperations) errorResult(err error) (*controller.OperationResult, error) {
	updatedStatus, _ := json.Marshal(h.status)

	// Always treat as transient - return error to trigger retry
	return &controller.OperationResult{
		UpdatedStatus: updatedStatus,
		Success:       false,
	}, err
}

// pendingResult creates an OperationResult for operations still in progress.
// Returns the result and nil error, matching the ComponentOperations method signatures.
func (h *HelmOperations) pendingResult() (*controller.OperationResult, error) {
	updatedStatus, _ := json.Marshal(h.status)
	return &controller.OperationResult{
		UpdatedStatus: updatedStatus,
		Success:       false,
	}, nil
}

// gatherHelmReleaseResources extracts Kubernetes resources from a Helm release manifest
// and builds a ResourceList for status checking
func (h *HelmOperations) gatherHelmReleaseResources(ctx context.Context, rel *release.Release) (kube.ResourceList, error) {
	log := logf.FromContext(ctx).WithValues("releaseName", rel.Name)

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

	log.Info("Built resource list from release manifest", "resourceCount", len(resourceList))

	return resourceList, nil
}
