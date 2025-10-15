// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

// config.go contains Helm configuration parsing and validation logic.
// This includes the HelmConfig struct definition and related parsing functions
// that handle Component.Spec.Config unmarshaling for helm components.

package helm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-playground/validator/v10"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// helmConfigValidator is a package-level validator instance that is reused across all reconciliations.
// validator.Validator is thread-safe and designed for concurrent use, making this safe to share.
var helmConfigValidator = validator.New()

// HelmConfig represents the configuration structure for Helm components
// that gets unmarshaled from Component.Spec.Config.
//
// Note: Source configuration is now handled separately by the source registry.
// Each source parses its own configuration via ParseAndValidate.
type HelmConfig struct {
	// ReleaseName specifies the release name for Helm deployment
	ReleaseName string `json:"releaseName" validate:"required"`

	// ReleaseNamespace specifies the namespace for Helm release deployment
	ReleaseNamespace string `json:"releaseNamespace" validate:"required"`

	// ManageNamespace controls whether the handler creates and deletes the release namespace
	// When true: Creates namespace on install, deletes namespace on uninstall (if empty)
	// When false: Assumes namespace exists, leaves it untouched on uninstall
	// Default: true (sensible default for most use cases)
	// +optional
	ManageNamespace *bool `json:"manageNamespace,omitempty"`

	// Values contains key-value pairs for chart values override
	// +optional
	Values map[string]any `json:"values,omitempty"`
}

// HelmStatus contains handler-specific status data for Helm deployments.
// This data is persisted across reconciliation loops in Component.Status.HandlerStatus.
// After initial deployment, operations use the persisted values rather than spec values
// to ensure consistency with what was actually deployed.
type HelmStatus struct {
	// ReleaseVersion tracks the current Helm release version
	ReleaseVersion int `json:"releaseVersion,omitempty"`

	// LastDeployTime records when the deployment was last initiated
	LastDeployTime string `json:"lastDeployTime,omitempty"`

	// ChartVersion tracks the deployed chart version
	ChartVersion string `json:"chartVersion,omitempty"`

	// ReleaseName tracks the actual release name used for deployment
	// Once set, all subsequent operations (upgrade, delete, status checks) use this name
	// instead of the spec value to ensure consistency with the deployed release
	ReleaseName string `json:"releaseName,omitempty"`
}

// resolveHelmConfig unmarshals Component.Spec.Config into HelmConfig struct.
// Source configuration is handled separately by the source registry via ParseAndValidate.
func resolveHelmConfig(ctx context.Context, rawConfig json.RawMessage) (*HelmConfig, error) {
	// Parse config fields (source is handled separately)
	var config HelmConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to parse helm config: %w", err)
	}

	// Validate configuration using shared validator instance
	if err := helmConfigValidator.Struct(&config); err != nil {
		return nil, fmt.Errorf("helm config validation failed: %w", err)
	}

	// Set sensible default for namespace management
	// Default to true - most users want full namespace lifecycle management
	if config.ManageNamespace == nil {
		defaultManageNamespace := true
		config.ManageNamespace = &defaultManageNamespace
	}

	log := logf.FromContext(ctx).WithValues(
		"releaseName", config.ReleaseName,
		"namespace", config.ReleaseNamespace,
		"valuesCount", len(config.Values))
	log.V(1).Info("Resolved helm config")

	return &config, nil
}

func resolveHelmStatus(ctx context.Context, rawStatus json.RawMessage) (*HelmStatus, error) {
	status := &HelmStatus{}
	if len(rawStatus) == 0 {
		logf.FromContext(ctx).V(1).Info("No existing helm status found, starting with empty status")
		return status, nil
	}

	if err := json.Unmarshal(rawStatus, &status); err != nil {
		return nil, err
	}

	return status, nil
}
