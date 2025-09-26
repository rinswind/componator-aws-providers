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
	"encoding/json"
	"fmt"

	"github.com/go-playground/validator/v10"
	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

// HelmConfig represents the configuration structure for Helm components
// that gets unmarshaled from Component.Spec.Config
type HelmConfig struct {
	// Repository specifies the Helm chart repository configuration
	Repository HelmRepository `json:"repository"`

	// Chart specifies the chart name and version to deploy
	Chart HelmChart `json:"chart"`

	// ReleaseName specifies the release name for Helm deployment
	ReleaseName string `json:"releaseName" validate:"required"`

	// Values contains key-value pairs for chart values override
	// +optional
	Values map[string]any `json:"values,omitempty"`

	// Namespace specifies the target namespace for chart deployment
	// If not specified, uses the Component's namespace
	// +optional
	Namespace string `json:"namespace,omitempty"`
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

// resolveHelmConfig unmarshals Component.Spec.Config into HelmConfig struct
// and resolves the target namespace (fills in from Component.Namespace if not specified)
func resolveHelmConfig(component *deploymentsv1alpha1.Component) (*HelmConfig, error) {
	if component.Spec.Config == nil {
		return nil, fmt.Errorf("config is required for helm components")
	}

	var config HelmConfig
	if err := json.Unmarshal(component.Spec.Config.Raw, &config); err != nil {
		return nil, fmt.Errorf("failed to parse helm config: %w", err)
	}

	// Validate configuration using validator framework
	validate := validator.New()
	if err := validate.Struct(&config); err != nil {
		return nil, fmt.Errorf("helm config validation failed: %w", err)
	}

	// Resolve target namespace: use configured namespace or fall back to Component's namespace
	if config.Namespace == "" {
		config.Namespace = component.Namespace
	}

	return &config, nil
}
