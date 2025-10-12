// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOCIReference(t *testing.T) {
	tests := []struct {
		name            string
		ref             string
		expectRegistry  string
		expectChartPath string
		expectVersion   string
		expectError     bool
	}{
		{
			name:            "valid full reference",
			ref:             "oci://ghcr.io/my-org/my-chart:1.0.0",
			expectRegistry:  "ghcr.io",
			expectChartPath: "my-org/my-chart",
			expectVersion:   "1.0.0",
			expectError:     false,
		},
		{
			name:            "valid with nested path",
			ref:             "oci://registry.example.com/org/team/chart:2.3.4",
			expectRegistry:  "registry.example.com",
			expectChartPath: "org/team/chart",
			expectVersion:   "2.3.4",
			expectError:     false,
		},
		{
			name:            "valid minimal",
			ref:             "oci://r.io/chart:v1",
			expectRegistry:  "r.io",
			expectChartPath: "chart",
			expectVersion:   "v1",
			expectError:     false,
		},
		{
			name:        "missing oci prefix",
			ref:         "ghcr.io/my-org/my-chart:1.0.0",
			expectError: true,
		},
		{
			name:        "missing version",
			ref:         "oci://ghcr.io/my-org/my-chart",
			expectError: true,
		},
		{
			name:        "missing path",
			ref:         "oci://ghcr.io:1.0.0",
			expectError: true,
		},
		{
			name:        "empty reference",
			ref:         "",
			expectError: true,
		},
		{
			name:        "multiple colons",
			ref:         "oci://ghcr.io/chart:1.0.0:extra",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, chartPath, version, err := parseOCIReference(tt.ref)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectRegistry, registry)
			assert.Equal(t, tt.expectChartPath, chartPath)
			assert.Equal(t, tt.expectVersion, version)
		})
	}
}

func TestSecretRef(t *testing.T) {
	tests := []struct {
		name      string
		secretRef *SecretRef
		validate  func(t *testing.T, ref *SecretRef)
	}{
		{
			name: "valid secret reference",
			secretRef: &SecretRef{
				Name:      "ghcr-credentials",
				Namespace: "default",
			},
			validate: func(t *testing.T, ref *SecretRef) {
				assert.Equal(t, "ghcr-credentials", ref.Name)
				assert.Equal(t, "default", ref.Namespace)
			},
		},
		{
			name:      "nil secret reference",
			secretRef: nil,
			validate: func(t *testing.T, ref *SecretRef) {
				assert.Nil(t, ref)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.validate(t, tt.secretRef)
		})
	}
}

func TestNewChartSource(t *testing.T) {
	chartRef := "oci://ghcr.io/my-org/my-chart:1.0.0"
	secretRef := &SecretRef{
		Name:      "credentials",
		Namespace: "default",
	}

	source := NewChartSource(chartRef, secretRef, nil, nil)

	assert.NotNil(t, source)
	assert.Equal(t, chartRef, source.chartRef)
	assert.Equal(t, secretRef, source.secretRef)
}
