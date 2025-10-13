// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

// Package http implements HTTP repository chart source for the plugin architecture.
// This source wraps the singleton CachingRepository and implements per-reconciliation
// configuration via ParseAndValidate.
package http

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-playground/validator/v10"
	"helm.sh/helm/v3/pkg/cli"
)

// Source implements the ChartSource interface for HTTP Helm repositories.
// This is a long-lived singleton that wraps the shared CachingRepository.
// Configuration is provided per reconciliation via ParseAndValidate.
type Source struct {
	httpRepo *CachingRepository // Singleton repository with index caching
	config   *Config            // Current configuration from last ParseAndValidate call
}

// NewSource creates a new HTTP chart source wrapping the singleton CachingRepository.
// The source is stateless until ParseAndValidate is called with configuration.
func NewSource(httpRepo *CachingRepository) *Source {
	return &Source{
		httpRepo: httpRepo,
	}
}

// Type returns the source type identifier for registry lookup.
func (s *Source) Type() string {
	return "http"
}

// ParseAndValidate parses and validates HTTP source configuration from raw JSON.
// This extracts the "source" section and validates the HTTP-specific fields.
//
// Expected JSON structure:
//
//	{
//	  "releaseName": "...",
//	  "releaseNamespace": "...",
//	  "source": {
//	    "type": "http",
//	    "repository": {"url": "...", "name": "..."},
//	    "chart": {"name": "...", "version": "..."}
//	  }
//	}
func (s *Source) ParseAndValidate(ctx context.Context, rawConfig json.RawMessage) error {
	// Parse the raw config to extract the source section
	var configMap map[string]json.RawMessage
	if err := json.Unmarshal(rawConfig, &configMap); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	// Extract source section
	rawSource, hasSource := configMap["source"]
	if !hasSource {
		return fmt.Errorf("source field is required")
	}

	// Parse source configuration
	var config Config
	if err := json.Unmarshal(rawSource, &config); err != nil {
		return fmt.Errorf("failed to parse HTTP source configuration: %w", err)
	}

	// Validate configuration
	validate := validator.New()
	if err := validate.Struct(&config); err != nil {
		return fmt.Errorf("HTTP source validation failed: %w", err)
	}

	// Store validated configuration
	s.config = &config

	return nil
}

// LocateChart retrieves a Helm chart from the HTTP repository and returns the path
// to the cached chart file.
//
// This delegates to the singleton CachingRepository which handles:
//   - Repository index caching (in-memory and on-disk)
//   - Chart downloading to Helm cache
func (s *Source) LocateChart(ctx context.Context, settings *cli.EnvSettings) (string, error) {
	if s.config == nil {
		return "", fmt.Errorf("ParseAndValidate must be called before LocateChart")
	}

	return s.httpRepo.LocateChart(
		s.config.Repository.Name,
		s.config.Repository.URL,
		s.config.Chart.Name,
		s.config.Chart.Version,
		settings,
	)
}

// GetVersion returns the configured chart version from the last ParseAndValidate call.
func (s *Source) GetVersion() string {
	if s.config == nil {
		return ""
	}
	return s.config.Chart.Version
}
