// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package sources

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/cli"
)

// mockFactory implements ChartSourceFactory for testing
type mockFactory struct {
	sourceType string
	version    string
}

func (m *mockFactory) Type() string {
	return m.sourceType
}

func (m *mockFactory) CreateSource(ctx context.Context, rawConfig json.RawMessage, settings *cli.EnvSettings) (ChartSource, error) {
	return &mockChartSource{version: m.version}, nil
}

// mockChartSource implements ChartSource for testing
type mockChartSource struct {
	version string
}

func (m *mockChartSource) LocateChart(ctx context.Context) (string, error) {
	return "", nil
}

func (m *mockChartSource) GetVersion() string {
	return m.version
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	registry := NewRegistry()

	httpFactory := &mockFactory{sourceType: "http", version: "1.0.0"}
	ociFactory := &mockFactory{sourceType: "oci", version: "2.0.0"}

	// Register factories using Register method
	registry.Register(httpFactory)
	registry.Register(ociFactory)

	// Test successful retrieval
	t.Run("get registered http factory", func(t *testing.T) {
		factory, err := registry.Get("http")
		require.NoError(t, err)
		assert.Equal(t, "http", factory.Type())
	})

	t.Run("get registered oci factory", func(t *testing.T) {
		factory, err := registry.Get("oci")
		require.NoError(t, err)
		assert.Equal(t, "oci", factory.Type())
	})

	// Test unknown source type
	t.Run("get unknown source type", func(t *testing.T) {
		factory, err := registry.Get("git")
		assert.Error(t, err)
		assert.Nil(t, factory)
		assert.Contains(t, err.Error(), "unknown source type: git")
	})
}

func TestRegistry_CreateSource(t *testing.T) {
	ctx := context.Background()
	settings := cli.New()

	registry := NewRegistry()
	httpFactory := &mockFactory{sourceType: "http", version: "1.0.0"}
	ociFactory := &mockFactory{sourceType: "oci", version: "2.0.0"}

	registry.Register(httpFactory)
	registry.Register(ociFactory)

	tests := []struct {
		name            string
		rawConfig       string
		expectedVersion string
		expectError     bool
		errorMsg        string
	}{
		{
			name: "http source type - composite pattern delegates to http factory",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"type": "http",
					"repository": {"url": "https://charts.example.com", "name": "example"},
					"chart": {"name": "mychart", "version": "1.0.0"}
				}
			}`,
			expectedVersion: "1.0.0",
			expectError:     false,
		},
		{
			name: "oci source type - composite pattern delegates to oci factory",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"type": "oci",
					"chart": "oci://ghcr.io/example/mychart:1.0.0"
				}
			}`,
			expectedVersion: "2.0.0",
			expectError:     false,
		},
		{
			name: "unknown source type",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"type": "git",
					"repository": "https://github.com/example/charts"
				}
			}`,
			expectError: true,
			errorMsg:    "unknown source type: git",
		},
		{
			name: "missing source field",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default"
			}`,
			expectError: true,
			errorMsg:    "source field is required",
		},
		{
			name: "missing type field",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"repository": {"url": "https://charts.example.com"}
				}
			}`,
			expectError: true,
			errorMsg:    "source.type is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chartSource, err := registry.CreateSource(ctx, json.RawMessage(tt.rawConfig), settings)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, chartSource)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
				require.NotNil(t, chartSource)
				assert.Equal(t, tt.expectedVersion, chartSource.GetVersion())
			}
		})
	}
}

func TestDetectSourceType(t *testing.T) {
	tests := []struct {
		name        string
		rawConfig   string
		expected    string
		expectError bool
		errorMsg    string
	}{
		{
			name: "http source type",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"type": "http",
					"repository": {"url": "https://charts.example.com", "name": "example"},
					"chart": {"name": "mychart", "version": "1.0.0"}
				}
			}`,
			expected:    "http",
			expectError: false,
		},
		{
			name: "oci source type",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"type": "oci",
					"chart": "oci://ghcr.io/example/mychart:1.0.0"
				}
			}`,
			expected:    "oci",
			expectError: false,
		},
		{
			name: "missing source field",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default"
			}`,
			expectError: true,
			errorMsg:    "source field is required",
		},
		{
			name: "missing type field",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"repository": {"url": "https://charts.example.com"}
				}
			}`,
			expectError: true,
			errorMsg:    "source.type is required",
		},
		{
			name: "empty type field",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"type": "",
					"repository": {"url": "https://charts.example.com"}
				}
			}`,
			expectError: true,
			errorMsg:    "source.type is required",
		},
		{
			name:        "invalid json",
			rawConfig:   `{invalid json}`,
			expectError: true,
			errorMsg:    "failed to parse config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceType, err := detectSourceType(json.RawMessage(tt.rawConfig))

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, sourceType)
			}
		})
	}
}
