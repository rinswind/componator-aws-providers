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

// config.go contains Helm configuration parsing and validation logic.
// This includes the HelmConfig struct definition and related parsing functions
// that handle Component.Spec.Config unmarshaling for helm components.

package helm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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

	// Repository specifies the Helm chart repository configuration
	Repository HelmRepository `json:"repository"`

	// Chart specifies the chart name and version to deploy
	Chart HelmChart `json:"chart"`

	// Values contains key-value pairs for chart values override
	// +optional
	Values map[string]any `json:"values,omitempty"`

	// Timeouts contains timeout configuration for deployment and deletion operations
	// +optional
	Timeouts *HelmTimeouts `json:"timeouts,omitempty"`
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

// HelmTimeouts represents timeout configuration for Helm operations
type HelmTimeouts struct {
	// Deployment timeout - how long to wait for Helm release to become ready
	// Transitions to Failed when exceeded
	// +optional
	Deployment *Duration `json:"deployment,omitempty"`

	// Deletion timeout - informational threshold for deletion visibility
	// Updates status message only, never blocks deletion
	// +optional
	Deletion *Duration `json:"deletion,omitempty"`
}

// Duration wraps time.Duration with JSON marshaling support
type Duration struct {
	time.Duration
}

// UnmarshalJSON implements json.Unmarshaler interface for Duration
func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	dur, err := time.ParseDuration(s)
	if err != nil {
		return err
	}

	d.Duration = dur
	return nil
}

// resolveHelmConfig unmarshals Component.Spec.Config into HelmConfig struct
// and resolves the target namespace (fills in from Component.Namespace if not specified)
func resolveHelmConfig(ctx context.Context, rawConfig json.RawMessage) (*HelmConfig, error) {
	var config HelmConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to parse helm config: %w", err)
	}

	// Validate configuration using validator framework
	validate := validator.New()
	if err := validate.Struct(&config); err != nil {
		return nil, fmt.Errorf("helm config validation failed: %w", err)
	}

	// Resolve timeouts
	if config.Timeouts == nil {
		config.Timeouts = &HelmTimeouts{}
	}

	// Set defaults if not specified
	// TODO: make the defaults configurable
	if config.Timeouts.Deployment == nil {
		config.Timeouts.Deployment = &Duration{Duration: 5 * time.Minute}
	}
	if config.Timeouts.Deletion == nil {
		config.Timeouts.Deletion = &Duration{Duration: 5 * time.Minute}
	}

	// Set sensible default for namespace management
	// Default to true - most users want full namespace lifecycle management
	if config.ManageNamespace == nil {
		defaultManageNamespace := true
		config.ManageNamespace = &defaultManageNamespace
	}

	log := logf.FromContext(ctx)

	log.V(1).Info("Resolved helm config",
		"repository", config.Repository.URL,
		"chart", config.Chart.Name,
		"version", config.Chart.Version,
		"releaseName", config.ReleaseName,
		"releaseNamespace", config.ReleaseNamespace,
		"valuesCount", len(config.Values))

	return &config, nil
}
