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

// HelmConfig represents the configuration structure for Helm components
// that gets unmarshaled from Component.Spec.Config
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

	// Source specifies the chart source configuration (HTTP repository or OCI registry)
	// This is a polymorphic field that can be either HTTPSource or OCISource
	// The source type is determined by the "type" field in the configuration
	Source SourceConfig `json:"-"` // Handled with custom unmarshaling

	// Values contains key-value pairs for chart values override
	// +optional
	Values map[string]any `json:"values,omitempty"`
}

// GetHTTPSource returns the HTTPSource if the source type is HTTP, nil otherwise.
// Temporary helper method until Phase 3 interface simplification.
func (c *HelmConfig) GetHTTPSource() *HTTPSource {
	if httpSource, ok := c.Source.(*HTTPSource); ok {
		return httpSource
	}
	return nil
}

// GetOCISource returns the OCISource if the source type is OCI, nil otherwise.
// Temporary helper method until Phase 3 interface simplification.
func (c *HelmConfig) GetOCISource() *OCISource {
	if ociSource, ok := c.Source.(*OCISource); ok {
		return ociSource
	}
	return nil
}

// HelmRepository represents Helm chart repository configuration
type HelmRepository struct {
	// URL is the chart repository URL
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^https?://.*`
	URL string `json:"url" validate:"required,url"`

	// Name is the repository name for local reference
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name" validate:"required,min=1"`
}

// HelmChart represents chart identification and version specification
type HelmChart struct {
	// Name is the chart name within the repository
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name" validate:"required,min=1"`

	// Version specifies the chart version to install
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version" validate:"required,min=1"`
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

// resolveHelmConfig unmarshals Component.Spec.Config into HelmConfig struct
// and resolves the source configuration using two-stage parsing.
func resolveHelmConfig(ctx context.Context, rawConfig json.RawMessage) (*HelmConfig, error) {
	log := logf.FromContext(ctx)

	// Parse the raw config into a temporary structure to extract source
	var rawConfigMap map[string]json.RawMessage
	if err := json.Unmarshal(rawConfig, &rawConfigMap); err != nil {
		return nil, fmt.Errorf("failed to parse helm config: %w", err)
	}

	// Ensure source field exists (required in new schema)
	rawSource, hasSource := rawConfigMap["source"]
	if !hasSource {
		return nil, fmt.Errorf("helm config validation failed: source field is required (must specify 'http' or 'oci' source)")
	}

	// Parse source configuration (two-stage parsing with type detection)
	source, err := resolveSourceConfig(rawSource)
	if err != nil {
		return nil, fmt.Errorf("failed to parse source configuration: %w", err)
	}

	// Parse remaining config fields
	var config HelmConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to parse helm config: %w", err)
	}

	// Inject resolved source
	config.Source = source

	// Validate configuration using validator framework
	validate := validator.New()
	if err := validate.Struct(&config); err != nil {
		return nil, fmt.Errorf("helm config validation failed: %w", err)
	}

	// Set sensible default for namespace management
	// Default to true - most users want full namespace lifecycle management
	if config.ManageNamespace == nil {
		defaultManageNamespace := true
		config.ManageNamespace = &defaultManageNamespace
	}

	log.V(1).Info("Resolved helm config",
		"sourceType", source.GetType(),
		"releaseName", config.ReleaseName,
		"releaseNamespace", config.ReleaseNamespace,
		"valuesCount", len(config.Values))

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
