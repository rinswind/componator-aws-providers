// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-playground/validator/v10"
	"github.com/rinswind/deployment-operator-handlers/internal/controller/helm/sources"
	"helm.sh/helm/v3/pkg/cli"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Factory implements the ChartSourceFactory interface for OCI registries.
// This is a stateless singleton that creates OCISource instances with validated configuration.
type Factory struct {
	k8sClient       client.Client // Kubernetes client for secret resolution (thread-safe)
	repositoryCache string        // Path to Helm repository cache directory
}

// NewFactory creates a new OCI chart source factory with Kubernetes client for secret resolution
// and repository cache path.
//
// Parameters:
//   - k8sClient: Kubernetes client for resolving registry credentials from secrets
//   - repositoryCache: Path to Helm repository cache directory (e.g., "/helm/repository")
func NewFactory(k8sClient client.Client, repositoryCache string) *Factory {
	return &Factory{
		k8sClient:       k8sClient,
		repositoryCache: repositoryCache,
	}
}

// Type returns the source type identifier for registry lookup.
func (f *Factory) Type() string {
	return "oci"
}

// CreateSource parses and validates OCI source configuration, then creates an immutable OCISource instance.
// The rawConfig parameter contains only the source section (already extracted by composite Registry).
// Validates OCI-specific fields including OCI reference format.
//
// Expected JSON structure:
//
//	{
//	  "type": "oci",
//	  "chart": "oci://registry.example.com/path/chart:version",
//	  "authentication": {
//	    "method": "registry",
//	    "secretRef": {"name": "...", "namespace": "..."}
//	  }
//	}
func (f *Factory) CreateSource(ctx context.Context, rawConfig json.RawMessage, settings *cli.EnvSettings) (sources.ChartSource, error) {
	// Parse source configuration (rawConfig is already the source section)
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to parse OCI source configuration: %w", err)
	}

	// Validate configuration with custom OCI reference validator
	validate := validator.New()
	if err := validate.RegisterValidation("oci_reference", validateOCIReference); err != nil {
		return nil, fmt.Errorf("failed to register OCI validator: %w", err)
	}

	if err := validate.Struct(&config); err != nil {
		return nil, fmt.Errorf("OCI source validation failed: %w", err)
	}

	// Create immutable source instance with validated configuration
	return OCISource{
		k8sClient:       f.k8sClient,
		repositoryCache: f.repositoryCache,
		config:          config,
		settings:        settings,
	}, nil
}
