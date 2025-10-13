// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

// Package http implements HTTP repository chart source for the plugin architecture.
// This source is created per-reconciliation by the Factory with immutable configuration.
package http

import (
	"context"

	"helm.sh/helm/v3/pkg/cli"
)

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
