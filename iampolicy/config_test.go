// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package iampolicy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveSpec(t *testing.T) {
	t.Run("valid configuration with all fields", func(t *testing.T) {
		config := &IamPolicyConfig{
			PolicyName:     "test-policy",
			PolicyDocument: "{\"Version\": \"2012-10-17\", \"Statement\": []}",
			Description:    "Test policy",
			Path:           "/test/",
			Tags:           map[string]string{"env": "test"},
		}

		err := resolveSpec(config)
		require.NoError(t, err)
		assert.Equal(t, "test-policy", config.PolicyName)
		assert.Equal(t, "{\"Version\": \"2012-10-17\", \"Statement\": []}", config.PolicyDocument)
		assert.Equal(t, "Test policy", config.Description)
		assert.Equal(t, "/test/", config.Path)
		assert.Equal(t, map[string]string{"env": "test"}, config.Tags)
	})

	t.Run("valid configuration with minimal fields", func(t *testing.T) {
		config := &IamPolicyConfig{
			PolicyName:     "minimal-policy",
			PolicyDocument: "{\"Version\": \"2012-10-17\"}",
		}

		err := resolveSpec(config)
		require.NoError(t, err)
		assert.Equal(t, "minimal-policy", config.PolicyName)
		assert.Equal(t, "{\"Version\": \"2012-10-17\"}", config.PolicyDocument)
		assert.Equal(t, "/", config.Path) // default
		assert.Empty(t, config.Description)
		assert.Empty(t, config.Tags)
	})

	t.Run("missing policy name", func(t *testing.T) {
		config := &IamPolicyConfig{
			PolicyDocument: "{\"Version\": \"2012-10-17\"}",
		}

		err := resolveSpec(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "policyName is required")
	})

	t.Run("missing policy document", func(t *testing.T) {
		config := &IamPolicyConfig{
			PolicyName: "test-policy",
		}

		err := resolveSpec(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "policyDocument is required")
	})

	t.Run("invalid JSON policy document", func(t *testing.T) {
		config := &IamPolicyConfig{
			PolicyName:     "test-policy",
			PolicyDocument: "not valid json",
		}

		err := resolveSpec(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be valid JSON")
	})
}

func TestApplyDefaults(t *testing.T) {
	t.Run("applies default path", func(t *testing.T) {
		config := &IamPolicyConfig{
			PolicyName:     "test-policy",
			PolicyDocument: "{}",
		}

		err := applyDefaults(config)
		require.NoError(t, err)
		assert.Equal(t, "/", config.Path)
	})

	t.Run("preserves existing path", func(t *testing.T) {
		config := &IamPolicyConfig{
			PolicyName:     "test-policy",
			PolicyDocument: "{}",
			Path:           "/custom/",
		}

		err := applyDefaults(config)
		require.NoError(t, err)
		assert.Equal(t, "/custom/", config.Path)
	})
}
