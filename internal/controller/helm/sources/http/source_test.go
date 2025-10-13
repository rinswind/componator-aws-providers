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

func TestFactory_Type(t *testing.T) {
	factory := NewFactory(nil)
	assert.Equal(t, "http", factory.Type())
}

func TestFactory_CreateSource(t *testing.T) {
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
			factory := NewFactory(nil)
			ctx := context.Background()
			settings := cli.New()

			source, err := factory.CreateSource(ctx, json.RawMessage(tt.rawConfig), settings)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, source)
				// Cast to HTTPSource to verify internal config
				httpSource, ok := source.(HTTPSource)
				require.True(t, ok, "source should be HTTPSource type")
				assert.Equal(t, tt.wantConfig.Repository.URL, httpSource.config.Repository.URL)
				assert.Equal(t, tt.wantConfig.Repository.Name, httpSource.config.Repository.Name)
				assert.Equal(t, tt.wantConfig.Chart.Name, httpSource.config.Chart.Name)
				assert.Equal(t, tt.wantConfig.Chart.Version, httpSource.config.Chart.Version)
			}
		})
	}
}

func TestHTTPSource_GetVersion(t *testing.T) {
	factory := NewFactory(nil)
	ctx := context.Background()
	settings := cli.New()

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

	source, err := factory.CreateSource(ctx, json.RawMessage(rawConfig), settings)
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", source.GetVersion())
}
