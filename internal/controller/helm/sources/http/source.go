// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

// Package http implements HTTP repository chart source for the plugin architecture.
// This source is created per-reconciliation by the Factory with immutable configuration.
package http

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-playground/validator/v10"
	"github.com/rinswind/deployment-operator-handlers/internal/controller/helm/sources"
	"helm.sh/helm/v3/pkg/cli"
)

// Factory implements the ChartSourceFactory interface for HTTP Helm repositories.
// This is a stateless singleton that creates HTTPSource instances with validated configuration.
type Factory struct {
	httpRepo *CachingRepository // Singleton repository with index caching (thread-safe)
}

// NewFactory creates a new HTTP chart source factory wrapping the singleton CachingRepository.
// The factory is stateless and thread-safe.
func NewFactory(httpRepo *CachingRepository) *Factory {
	return &Factory{
		httpRepo: httpRepo,
	}
}

// Type returns the source type identifier for registry lookup.
func (f *Factory) Type() string {
	return "http"
}

// CreateSource parses and validates HTTP source configuration, then creates an immutable HTTPSource instance.
// The rawConfig parameter contains only the source section (already extracted by composite Registry).
//
// Expected JSON structure:
//
//	{
//	  "type": "http",
//	  "repository": {"url": "...", "name": "..."},
//	  "chart": {"name": "...", "version": "..."}
//	}
func (f *Factory) CreateSource(ctx context.Context, rawConfig json.RawMessage, settings *cli.EnvSettings) (sources.ChartSource, error) {
	// Parse source configuration (rawConfig is already the source section)
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to parse HTTP source configuration: %w", err)
	}

	// Validate configuration
	validate := validator.New()
	if err := validate.Struct(&config); err != nil {
		return nil, fmt.Errorf("HTTP source validation failed: %w", err)
	}

	// Create immutable source instance with validated configuration
	return HTTPSource{
		httpRepo: f.httpRepo,
		config:   config,
		settings: settings,
	}, nil
}

// HTTPSource implements the ChartSource interface for HTTP Helm repositories.
// This is a per-reconciliation instance created by Factory with immutable configuration.
// All configuration is baked in at creation time, making it thread-safe.
type HTTPSource struct {
	httpRepo *CachingRepository // Singleton repository with index caching (thread-safe)
	config   Config             // Immutable configuration (no pointer, value type)
	settings *cli.EnvSettings   // Immutable settings (no mutation after creation)
}

// LocateChart retrieves a Helm chart from the HTTP repository and returns the path
// to the cached chart file.
//
// This delegates to the singleton CachingRepository which handles:
//   - Repository index caching (in-memory and on-disk)
//   - Chart downloading to Helm cache
func (s HTTPSource) LocateChart(ctx context.Context) (string, error) {
	return s.httpRepo.LocateChart(
		s.config.Repository.Name,
		s.config.Repository.URL,
		s.config.Chart.Name,
		s.config.Chart.Version,
		s.settings,
	)
}

// GetVersion returns the configured chart version.
func (s HTTPSource) GetVersion() string {
	return s.config.Chart.Version
}
