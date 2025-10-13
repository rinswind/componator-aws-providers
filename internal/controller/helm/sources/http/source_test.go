// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package http

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/cli"
)

func TestSource_Type(t *testing.T) {
	source := NewSource(nil)
	assert.Equal(t, "http", source.Type())
}

func TestSource_ParseAndValidate(t *testing.T) {
	tests := []struct {
		name        string
		rawConfig   string
		expectError bool
		errorMsg    string
		wantConfig  *Config
	}{
		{
			name: "valid http source configuration",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"type": "http",
					"repository": {
						"url": "https://charts.example.com",
						"name": "example"
					},
					"chart": {
						"name": "mychart",
						"version": "1.0.0"
					}
				}
			}`,
			expectError: false,
			wantConfig: &Config{
				Repository: RepositoryConfig{
					URL:  "https://charts.example.com",
					Name: "example",
				},
				Chart: ChartConfig{
					Name:    "mychart",
					Version: "1.0.0",
				},
			},
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
			name: "missing repository field",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"type": "http",
					"chart": {
						"name": "mychart",
						"version": "1.0.0"
					}
				}
			}`,
			expectError: true,
			errorMsg:    "validation failed",
		},
		{
			name: "missing chart field",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"type": "http",
					"repository": {
						"url": "https://charts.example.com",
						"name": "example"
					}
				}
			}`,
			expectError: true,
			errorMsg:    "validation failed",
		},
		{
			name: "invalid repository URL",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"type": "http",
					"repository": {
						"url": "not-a-url",
						"name": "example"
					},
					"chart": {
						"name": "mychart",
						"version": "1.0.0"
					}
				}
			}`,
			expectError: true,
			errorMsg:    "validation failed",
		},
		{
			name: "empty chart name",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"type": "http",
					"repository": {
						"url": "https://charts.example.com",
						"name": "example"
					},
					"chart": {
						"name": "",
						"version": "1.0.0"
					}
				}
			}`,
			expectError: true,
			errorMsg:    "validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := NewSource(nil)
			ctx := context.Background()

			err := source.ParseAndValidate(ctx, json.RawMessage(tt.rawConfig))

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantConfig.Repository.URL, source.config.Repository.URL)
				assert.Equal(t, tt.wantConfig.Repository.Name, source.config.Repository.Name)
				assert.Equal(t, tt.wantConfig.Chart.Name, source.config.Chart.Name)
				assert.Equal(t, tt.wantConfig.Chart.Version, source.config.Chart.Version)
			}
		})
	}
}

func TestSource_GetVersion(t *testing.T) {
	t.Run("returns empty before ParseAndValidate", func(t *testing.T) {
		source := NewSource(nil)
		assert.Equal(t, "", source.GetVersion())
	})

	t.Run("returns version after ParseAndValidate", func(t *testing.T) {
		source := NewSource(nil)
		ctx := context.Background()

		rawConfig := `{
			"releaseName": "my-release",
			"releaseNamespace": "default",
			"source": {
				"type": "http",
				"repository": {
					"url": "https://charts.example.com",
					"name": "example"
				},
				"chart": {
					"name": "mychart",
					"version": "1.2.3"
				}
			}
		}`

		err := source.ParseAndValidate(ctx, json.RawMessage(rawConfig))
		require.NoError(t, err)
		assert.Equal(t, "1.2.3", source.GetVersion())
	})
}

func TestSource_LocateChart_RequiresParseAndValidate(t *testing.T) {
	source := NewSource(nil)
	ctx := context.Background()
	settings := cli.New()

	_, err := source.LocateChart(ctx, settings)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ParseAndValidate must be called before LocateChart")
}
