// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

// source_config.go defines the polymorphic source configuration types for Helm charts.
// This supports multiple chart sources (HTTP repositories, OCI registries) through
// a unified SourceConfig interface with type-based routing.

package helm

import (
	"encoding/json"
	"fmt"

	"github.com/go-playground/validator/v10"
)

// SourceConfig provides a polymorphic interface for different chart source types.
// Implementations include HTTPSource (traditional Helm repositories) and OCISource
// (OCI registry support).
type SourceConfig interface {
	// GetType returns the source type identifier ("http" or "oci")
	GetType() string

	// GetAuthentication returns type-specific authentication configuration
	// Returns nil if no authentication is configured
	GetAuthentication() interface{}
}

// HTTPSource represents a traditional HTTP Helm chart repository source.
// This is the existing configuration format wrapped as a typed source.
type HTTPSource struct {
	// Type must be "http"
	Type string `json:"type" validate:"required,eq=http"`

	// Repository specifies the Helm chart repository configuration
	Repository HelmRepository `json:"repository" validate:"required"`

	// Chart specifies the chart name and version to deploy
	Chart HelmChart `json:"chart" validate:"required"`

	// Authentication contains optional HTTP repository authentication
	// +optional
	Authentication *HTTPAuthentication `json:"authentication,omitempty"`
}

func (h *HTTPSource) GetType() string {
	return h.Type
}

func (h *HTTPSource) GetAuthentication() interface{} {
	return h.Authentication
}

// HTTPAuthentication contains authentication configuration for HTTP repositories.
// Currently reserved for future implementation.
type HTTPAuthentication struct {
	// Future: username/password, token-based auth
}

// OCISource represents an OCI registry chart source.
// Charts are addressed using OCI references: oci://registry.example.com/path/to/chart:version
type OCISource struct {
	// Type must be "oci"
	Type string `json:"type" validate:"required,eq=oci"`

	// Chart is the full OCI reference including registry, path, and version
	// Format: oci://registry.example.com/path/to/chart:version
	// Example: oci://ghcr.io/my-org/my-chart:1.0.0
	Chart string `json:"chart" validate:"required,oci_reference"`

	// Authentication contains optional OCI registry authentication
	// +optional
	Authentication *OCIAuthentication `json:"authentication,omitempty"`
}

func (o *OCISource) GetType() string {
	return o.Type
}

func (o *OCISource) GetAuthentication() interface{} {
	return o.Authentication
}

// OCIAuthentication contains authentication configuration for OCI registries.
type OCIAuthentication struct {
	// Method specifies the authentication method (currently only "registry" is supported)
	Method string `json:"method" validate:"required,eq=registry"`

	// SecretRef references a Kubernetes secret containing registry credentials
	SecretRef SecretRef `json:"secretRef" validate:"required"`
}

// SecretRef references a Kubernetes secret for credential storage.
// Used across different source types for consistent credential management.
type SecretRef struct {
	// Name is the secret name
	Name string `json:"name" validate:"required"`

	// Namespace is the secret namespace
	// Should be either the Component's namespace or the system namespace (deployment-system)
	Namespace string `json:"namespace" validate:"required"`
}

// resolveSourceConfig implements two-stage parsing for polymorphic source configuration.
// Stage 1: Parse type field to determine source type
// Stage 2: Parse full schema based on detected type
func resolveSourceConfig(rawSource json.RawMessage) (SourceConfig, error) {
	// Stage 1: Detect source type
	var typeDetector struct {
		Type string `json:"type"`
	}

	if err := json.Unmarshal(rawSource, &typeDetector); err != nil {
		return nil, fmt.Errorf("failed to detect source type: %w", err)
	}

	if typeDetector.Type == "" {
		return nil, fmt.Errorf("source type is required (must be 'http' or 'oci')")
	}

	// Stage 2: Parse type-specific configuration
	validate := validator.New()

	// Register custom OCI reference validator
	if err := validate.RegisterValidation("oci_reference", validateOCIReference); err != nil {
		return nil, fmt.Errorf("failed to register OCI validator: %w", err)
	}

	switch typeDetector.Type {
	case "http":
		var httpSource HTTPSource
		if err := json.Unmarshal(rawSource, &httpSource); err != nil {
			return nil, fmt.Errorf("failed to parse HTTP source configuration: %w", err)
		}

		if err := validate.Struct(&httpSource); err != nil {
			return nil, fmt.Errorf("HTTP source validation failed: %w", err)
		}

		return &httpSource, nil

	case "oci":
		var ociSource OCISource
		if err := json.Unmarshal(rawSource, &ociSource); err != nil {
			return nil, fmt.Errorf("failed to parse OCI source configuration: %w", err)
		}

		if err := validate.Struct(&ociSource); err != nil {
			return nil, fmt.Errorf("OCI source validation failed: %w", err)
		}

		return &ociSource, nil

	default:
		return nil, fmt.Errorf("unsupported source type: %s (must be 'http' or 'oci')", typeDetector.Type)
	}
}

// validateOCIReference validates OCI chart reference format.
// Expected format: oci://registry.example.com/path/to/chart:version
func validateOCIReference(fl validator.FieldLevel) bool {
	ref := fl.Field().String()

	// Basic validation: must start with oci:// and contain a colon for version
	if len(ref) < 10 { // Minimum: oci://r:v
		return false
	}

	if ref[:6] != "oci://" {
		return false
	}

	// Must contain version separator (colon after registry path)
	// Note: More sophisticated validation could use OCI reference parsing libraries
	// For now, basic checks ensure well-formed references
	remainder := ref[6:]
	if len(remainder) == 0 {
		return false
	}

	// Must have at least one path component and a version
	// Format: registry/path:version
	hasSlash := false
	hasColon := false
	for _, ch := range remainder {
		if ch == '/' {
			hasSlash = true
		}
		if ch == ':' {
			hasColon = true
		}
	}

	return hasSlash && hasColon
}
