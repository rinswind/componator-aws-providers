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
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/rinswind/deployment-operator/handler/base"
)

const (
	// HandlerName is the identifier for this helm handler
	HandlerName = "helm"

	ControllerName = "helm-component"
)

// HelmOperationsFactory implements the ComponentOperationsFactory interface for Helm deployments.
// This factory creates stateful HelmOperations instances with pre-parsed configuration,
// eliminating repeated configuration parsing during reconciliation loops.
type HelmOperationsFactory struct{}

// CreateOperations creates a new stateful HelmOperations instance with pre-parsed configuration.
// This method is called once per reconciliation loop to eliminate repeated configuration parsing.
//
// The returned HelmOperations instance maintains the parsed configuration and can be used
// throughout the reconciliation loop without re-parsing the same configuration multiple times.
func (f *HelmOperationsFactory) CreateOperations(ctx context.Context, config json.RawMessage) (base.ComponentOperations, error) {
	// Parse configuration once for this reconciliation loop
	helmConfig, err := parseHelmConfigFromRaw(config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse helm configuration: %w", err)
	}

	// Return stateful operations instance with pre-parsed configuration
	return &HelmOperations{
		config: helmConfig,
	}, nil
}

// HelmOperations implements the ComponentOperations interface for Helm-based deployments.
// This struct provides all Helm-specific deployment, upgrade, and deletion operations
// with pre-parsed configuration maintained throughout the reconciliation loop.
//
// This is a stateful operations instance created by HelmOperationsFactory that eliminates
// repeated configuration parsing by maintaining parsed configuration state.
type HelmOperations struct {
	// config holds the pre-parsed Helm configuration for this reconciliation loop
	config *HelmConfig
}

// NewHelmOperationsFactory creates a new HelmOperationsFactory instance
func NewHelmOperationsFactory() *HelmOperationsFactory {
	return &HelmOperationsFactory{}
}

// NewHelmOperationsConfig creates a ComponentHandlerConfig for Helm with default settings
func NewHelmOperationsConfig() base.ComponentHandlerConfig {
	return base.DefaultComponentHandlerConfig(HandlerName, ControllerName)
}

// parseHelmConfigFromRaw parses raw JSON configuration into HelmConfig struct with defaults
// This replaces the old resolveHelmConfig function and works with raw JSON instead of Component
func parseHelmConfigFromRaw(rawConfig json.RawMessage) (*HelmConfig, error) {
	if len(rawConfig) == 0 {
		return nil, fmt.Errorf("helm configuration is required but not provided")
	}

	var config HelmConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal helm configuration: %w", err)
	}

	// Apply validation using validator framework (same as old resolveHelmConfig)
	validate := validator.New()
	if err := validate.Struct(&config); err != nil {
		return nil, fmt.Errorf("helm config validation failed: %w", err)
	}

	// With factory pattern, ReleaseNamespace must be explicitly specified in configuration
	// since we don't have access to Component.Namespace for fallback resolution
	if config.ReleaseNamespace == "" {
		return nil, fmt.Errorf("releaseNamespace is required in helm configuration when using factory pattern")
	}

	// Apply timeout defaults (same logic as old resolveHelmConfig)
	if config.Timeouts == nil {
		config.Timeouts = &HelmTimeouts{}
	}

	// Set defaults if not specified
	if config.Timeouts.Deployment == nil {
		config.Timeouts.Deployment = &Duration{Duration: 5 * time.Minute}
	}
	if config.Timeouts.Deletion == nil {
		config.Timeouts.Deletion = &Duration{Duration: 5 * time.Minute}
	}

	// Set sensible default for namespace management
	if config.ManageNamespace == nil {
		defaultManageNamespace := true
		config.ManageNamespace = &defaultManageNamespace
	}

	return &config, nil
}
