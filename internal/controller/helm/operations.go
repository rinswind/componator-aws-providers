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

	"github.com/rinswind/deployment-operator/handler/base"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/release"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// HelmStatus contains handler-specific status data for Helm deployments.
// This data is persisted across reconciliation loops in Component.Status.HandlerStatus.
type HelmStatus struct {
	// ReleaseVersion tracks the current Helm release version
	ReleaseVersion int `json:"releaseVersion,omitempty"`

	// LastDeployTime records when the deployment was last initiated
	LastDeployTime string `json:"lastDeployTime,omitempty"`

	// ChartVersion tracks the deployed chart version
	ChartVersion string `json:"chartVersion,omitempty"`

	// ReleaseName tracks the actual release name used
	ReleaseName string `json:"releaseName,omitempty"`
}

// HelmOperationsFactory implements the ComponentOperationsFactory interface for Helm deployments.
type HelmOperationsFactory struct{}

func (f *HelmOperationsFactory) CreateOperations(
	ctx context.Context, rawConfig json.RawMessage, currentStatus json.RawMessage) (base.ComponentOperations, error) {

	config, err := resolveHelmConfig(ctx, rawConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to parse helm configuration: %w", err)
	}

	// Parse existing handler status
	var status HelmStatus
	if len(currentStatus) > 0 {
		if err := json.Unmarshal(currentStatus, &status); err != nil {
			// Log parsing error but continue with empty status - this is not fatal
			log := logf.FromContext(ctx)
			log.Info("Failed to parse existing helm status, starting with empty status", "error", err)
		}
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
	}, nil
}

// HelmOperations implements the ComponentOperations interface for Helm-based deployments.
// This struct provides all Helm-specific deployment, upgrade, and deletion operations
// with pre-parsed configuration maintained throughout the reconciliation loop.
type HelmOperations struct {
	config *HelmConfig
	status HelmStatus

	settings     *cli.EnvSettings
	actionConfig *action.Configuration
}

// getHelmRelease verifies if a Helm release exists and returns it
func (h *HelmOperations) getHelmRelease(ctx context.Context) (*release.Release, error) {
	statusAction := action.NewStatus(h.actionConfig)

	rel, err := statusAction.Run(h.config.ReleaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to get release status: %w", err)
	}

	return rel, nil
}

// successResult creates an OperationResult for successful operations
func (h *HelmOperations) successResult() *base.OperationResult {
	updatedStatus, _ := json.Marshal(h.status)
	return &base.OperationResult{
		UpdatedStatus: updatedStatus,
		Success:       true,
	}
}

// errorResult creates an OperationResult for failed operations with error details
func (h *HelmOperations) errorResult(err error) *base.OperationResult {
	updatedStatus, _ := json.Marshal(h.status)
	return &base.OperationResult{
		UpdatedStatus:  updatedStatus,
		Success:        false,
		OperationError: err,
	}
}

// pendingResult creates an OperationResult for operations still in progress
func (h *HelmOperations) pendingResult() *base.OperationResult {
	updatedStatus, _ := json.Marshal(h.status)
	return &base.OperationResult{
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
