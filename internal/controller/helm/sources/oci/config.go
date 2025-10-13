// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
)

// ociSourceValidator is a package-level validator instance that is reused across all reconciliations.
// validator.Validator is thread-safe and designed for concurrent use, making this safe to share.
// Custom validators are registered in init() to ensure they're available before any validation occurs.
var ociSourceValidator = validator.New()

func init() {
	// Register custom OCI reference validator once at package initialization
	if err := ociSourceValidator.RegisterValidation("oci_reference", validateOCIReference); err != nil {
		panic(fmt.Sprintf("failed to register OCI validator: %v", err))
	}
}

// Config represents OCI registry configuration parsed from Component spec.
// This matches the existing JSON schema for backward compatibility.
type Config struct {
	// Chart is the full OCI reference including registry, path, and version
	// Format: oci://registry.example.com/path/to/chart:version
	Chart string `json:"chart" validate:"required,oci_reference"`

	// Authentication contains optional OCI registry authentication
	// +optional
	Authentication *AuthenticationConfig `json:"authentication,omitempty"`
}

// AuthenticationConfig contains authentication configuration for OCI registries.
type AuthenticationConfig struct {
	// Method specifies the authentication method (currently only "registry" is supported)
	Method string `json:"method" validate:"required,eq=registry"`

	// SecretRef references a Kubernetes secret containing registry credentials
	SecretRef SecretRefConfig `json:"secretRef" validate:"required"`
}

// SecretRefConfig references a Kubernetes secret for credential storage.
type SecretRefConfig struct {
	// Name is the secret name
	Name string `json:"name" validate:"required"`

	// Namespace is the secret namespace
	Namespace string `json:"namespace" validate:"required"`
}

// parseOCIReference parses an OCI chart reference into components.
// Input format: oci://registry.example.com/path/to/chart:version
// Returns: registry, chartPath, version
func parseOCIReference(ref string) (registry, chartPath, version string, err error) {
	if !strings.HasPrefix(ref, "oci://") {
		return "", "", "", fmt.Errorf("reference must start with oci://")
	}

	// Remove oci:// prefix
	remainder := ref[6:]

	// Split on : to separate version
	parts := strings.Split(remainder, ":")
	if len(parts) != 2 {
		return "", "", "", fmt.Errorf("reference must contain version (format: oci://registry/path:version)")
	}

	pathPart := parts[0]
	version = parts[1]

	// Split path to extract registry (first component)
	pathComponents := strings.Split(pathPart, "/")
	if len(pathComponents) < 2 {
		return "", "", "", fmt.Errorf("reference must contain registry and chart path")
	}

	registry = pathComponents[0]
	chartPath = strings.Join(pathComponents[1:], "/")

	return registry, chartPath, version, nil
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
