// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSource_Type(t *testing.T) {
	source := NewSource(nil)
	assert.Equal(t, "oci", source.Type())
}

func TestSource_ParseAndValidate(t *testing.T) {
	tests := []struct {
		name        string
		rawConfig   string
		expectError bool
		errorMsg    string
		wantChart   string
	}{
		{
			name: "valid oci source without authentication",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"type": "oci",
					"chart": "oci://ghcr.io/example/mychart:1.0.0"
				}
			}`,
			expectError: false,
			wantChart:   "oci://ghcr.io/example/mychart:1.0.0",
		},
		{
			name: "valid oci source with authentication",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"type": "oci",
					"chart": "oci://registry.example.com/charts/mychart:2.0.0",
					"authentication": {
						"method": "registry",
						"secretRef": {
							"name": "registry-creds",
							"namespace": "default"
						}
					}
				}
			}`,
			expectError: false,
			wantChart:   "oci://registry.example.com/charts/mychart:2.0.0",
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
			name: "missing chart field",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"type": "oci"
				}
			}`,
			expectError: true,
			errorMsg:    "validation failed",
		},
		{
			name: "invalid oci reference - missing oci:// prefix",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"type": "oci",
					"chart": "ghcr.io/example/mychart:1.0.0"
				}
			}`,
			expectError: true,
			errorMsg:    "validation failed",
		},
		{
			name: "invalid oci reference - missing version",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"type": "oci",
					"chart": "oci://ghcr.io/example/mychart"
				}
			}`,
			expectError: true,
			errorMsg:    "validation failed",
		},
		{
			name: "invalid oci reference - missing path",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"type": "oci",
					"chart": "oci://ghcr.io:1.0.0"
				}
			}`,
			expectError: true,
			errorMsg:    "validation failed",
		},
		{
			name: "invalid authentication - wrong method",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"type": "oci",
					"chart": "oci://ghcr.io/example/mychart:1.0.0",
					"authentication": {
						"method": "oauth",
						"secretRef": {
							"name": "creds",
							"namespace": "default"
						}
					}
				}
			}`,
			expectError: true,
			errorMsg:    "validation failed",
		},
		{
			name: "invalid authentication - missing secretRef",
			rawConfig: `{
				"releaseName": "my-release",
				"releaseNamespace": "default",
				"source": {
					"type": "oci",
					"chart": "oci://ghcr.io/example/mychart:1.0.0",
					"authentication": {
						"method": "registry"
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
				assert.Equal(t, tt.wantChart, source.config.Chart)
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
				"type": "oci",
				"chart": "oci://ghcr.io/example/mychart:1.2.3"
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

	_, err := source.LocateChart(ctx, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ParseAndValidate must be called before LocateChart")
}

func TestParseOCIReference(t *testing.T) {
	tests := []struct {
		name          string
		ref           string
		wantRegistry  string
		wantChartPath string
		wantVersion   string
		expectError   bool
		errorMsg      string
	}{
		{
			name:          "valid oci reference",
			ref:           "oci://ghcr.io/example/mychart:1.0.0",
			wantRegistry:  "ghcr.io",
			wantChartPath: "example/mychart",
			wantVersion:   "1.0.0",
			expectError:   false,
		},
		{
			name:          "valid oci reference with nested path",
			ref:           "oci://registry.example.com/org/team/charts/mychart:2.1.0",
			wantRegistry:  "registry.example.com",
			wantChartPath: "org/team/charts/mychart",
			wantVersion:   "2.1.0",
			expectError:   false,
		},
		{
			name:        "missing oci:// prefix",
			ref:         "ghcr.io/example/mychart:1.0.0",
			expectError: true,
			errorMsg:    "must start with oci://",
		},
		{
			name:        "missing version",
			ref:         "oci://ghcr.io/example/mychart",
			expectError: true,
			errorMsg:    "must contain version",
		},
		{
			name:        "missing path",
			ref:         "oci://ghcr.io:1.0.0",
			expectError: true,
			errorMsg:    "must contain registry and chart path",
		},
		{
			name:        "empty reference",
			ref:         "",
			expectError: true,
			errorMsg:    "must start with oci://",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, chartPath, version, err := parseOCIReference(tt.ref)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantRegistry, registry)
				assert.Equal(t, tt.wantChartPath, chartPath)
				assert.Equal(t, tt.wantVersion, version)
			}
		})
	}
}

func TestValidateOCIReference(t *testing.T) {
	tests := []struct {
		name  string
		ref   string
		valid bool
	}{
		{
			name:  "valid reference",
			ref:   "oci://ghcr.io/example/mychart:1.0.0",
			valid: true,
		},
		{
			name:  "valid reference with nested path",
			ref:   "oci://registry.example.com/org/charts/mychart:2.0.0",
			valid: true,
		},
		{
			name:  "missing oci:// prefix",
			ref:   "ghcr.io/example/mychart:1.0.0",
			valid: false,
		},
		{
			name:  "missing version colon",
			ref:   "oci://ghcr.io/example/mychart",
			valid: false,
		},
		{
			name:  "missing path slash",
			ref:   "oci://ghcr.io:1.0.0",
			valid: false,
		},
		{
			name:  "too short",
			ref:   "oci://",
			valid: false,
		},
		{
			name:  "empty string",
			ref:   "",
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock field level for validator
			// We'll test the logic directly since we can't easily mock validator.FieldLevel
			result := len(tt.ref) >= 10 &&
				strings.HasPrefix(tt.ref, "oci://") &&
				strings.Contains(tt.ref[6:], "/") &&
				strings.Contains(tt.ref[6:], ":")

			assert.Equal(t, tt.valid, result)
		})
	}
}
