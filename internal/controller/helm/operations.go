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
	"time"

	"github.com/rinswind/deployment-handlers/internal/controller/base"
	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

// HelmOperations implements the ComponentOperations interface for Helm-based deployments.
// This struct wraps the existing Helm-specific operation functions to work with the generic
// controller base while keeping all deployment logic unchanged.
type HelmOperations struct {
	// Future: could add Helm-specific configuration or clients here
}

// NewHelmOperations creates a new HelmOperations instance
func NewHelmOperations() *HelmOperations {
	return &HelmOperations{}
}

// Deploy initiates the initial deployment of a Helm Component's resources.
// Implements ComponentOperations.Deploy by delegating to existing startHelmReleaseDeployment.
func (h *HelmOperations) Deploy(ctx context.Context, component *deploymentsv1alpha1.Component) error {
	return startHelmReleaseDeployment(ctx, component)
}

// CheckDeployment verifies the current Helm deployment status and readiness.
// Implements ComponentOperations.CheckDeployment by delegating to existing checkReleaseDeployed.
func (h *HelmOperations) CheckDeployment(ctx context.Context, component *deploymentsv1alpha1.Component, elapsed time.Duration) (ready bool, ioError error, deploymentError error) {
	return checkReleaseDeployed(ctx, component, elapsed)
}

// Upgrade initiates an upgrade of an existing Helm deployment with new configuration.
// Implements ComponentOperations.Upgrade by delegating to existing startHelmReleaseUpgrade.
func (h *HelmOperations) Upgrade(ctx context.Context, component *deploymentsv1alpha1.Component) error {
	return startHelmReleaseUpgrade(ctx, component)
}

// Delete initiates cleanup/deletion of a Helm Component's resources.
// Implements ComponentOperations.Delete by delegating to existing startHelmReleaseDeletion.
func (h *HelmOperations) Delete(ctx context.Context, component *deploymentsv1alpha1.Component) error {
	return startHelmReleaseDeletion(ctx, component)
}

// CheckDeletion verifies the current Helm deletion status and completion.
// Implements ComponentOperations.CheckDeletion by delegating to existing checkHelmReleaseDeleted.
func (h *HelmOperations) CheckDeletion(ctx context.Context, component *deploymentsv1alpha1.Component, elapsed time.Duration) (deleted bool, ioError error, deletionError error) {
	return checkHelmReleaseDeleted(ctx, component, elapsed)
}

// HelmOperationsConfig implements the ComponentOperationsConfig interface for Helm-specific configuration.
// This provides the generic controller base with handler-specific settings while maintaining
// backward compatibility with existing Helm controller behavior.
type HelmOperationsConfig struct {
	// requeueSettings can be customized if needed, defaults to sensible values
	requeueSettings base.RequeueSettings
}

// NewHelmOperationsConfig creates a new HelmOperationsConfig with default settings
func NewHelmOperationsConfig() *HelmOperationsConfig {
	return &HelmOperationsConfig{
		requeueSettings: base.DefaultRequeueSettings(),
	}
}

// GetHandlerName returns the identifier for this helm handler
// Implements ComponentOperationsConfig.GetHandlerName
func (c *HelmOperationsConfig) GetHandlerName() string {
	return HandlerName
}

// GetControllerName returns the controller name for registration with the manager
// Implements ComponentOperationsConfig.GetControllerName
func (c *HelmOperationsConfig) GetControllerName() string {
	return ControllerName
}

// GetRequeueSettings returns timing configuration for requeue operations
// Implements ComponentOperationsConfig.GetRequeueSettings
func (c *HelmOperationsConfig) GetRequeueSettings() base.RequeueSettings {
	return c.requeueSettings
}

// SetRequeueSettings allows customization of requeue timing if needed
func (c *HelmOperationsConfig) SetRequeueSettings(settings base.RequeueSettings) {
	c.requeueSettings = settings
}
