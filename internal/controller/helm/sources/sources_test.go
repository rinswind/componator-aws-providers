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

// mockSource implements ChartSource for testing
type mockSource struct {
	sourceType string
	version    string
}

func (m *mockSource) Type() string {
	return m.sourceType
}

func (m *mockSource) ParseAndValidate(ctx context.Context, rawConfig json.RawMessage) error {
	return nil
}

func (m *mockSource) LocateChart(ctx context.Context, settings *cli.EnvSettings) (string, error) {
	return "", nil
}

func (m *mockSource) GetVersion() string {
	return m.version
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	registry := NewRegistry()

	httpSource := &mockSource{sourceType: "http", version: "1.0.0"}
	ociSource := &mockSource{sourceType: "oci", version: "2.0.0"}

	// Register sources (direct map assignment)
	registry["http"] = httpSource
	registry["oci"] = ociSource

	// Test successful retrieval
	t.Run("get registered http source", func(t *testing.T) {
		source, err := registry.Get("http")
		require.NoError(t, err)
		assert.Equal(t, "http", source.Type())
	})

	t.Run("get registered oci source", func(t *testing.T) {
		source, err := registry.Get("oci")
		require.NoError(t, err)
		assert.Equal(t, "oci", source.Type())
	})

	// Test unknown source type
	t.Run("get unknown source type", func(t *testing.T) {
		source, err := registry.Get("git")
		assert.Error(t, err)
		assert.Nil(t, source)
		assert.Contains(t, err.Error(), "unknown source type: git")
	})
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
			sourceType, err := DetectSourceType(json.RawMessage(tt.rawConfig))

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
