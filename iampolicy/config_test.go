// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package iampolicy

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveIamPolicyConfig(t *testing.T) {
	ctx := context.Background()

	t.Run("valid configuration with all fields", func(t *testing.T) {
		rawConfig := json.RawMessage(`{
			"policyName": "test-policy",
			"policyDocument": "{\"Version\": \"2012-10-17\", \"Statement\": []}",
			"description": "Test policy",
			"path": "/test/",
			"tags": {"env": "test"}
		}`)

		config, err := resolveIamPolicyConfig(ctx, rawConfig)
		require.NoError(t, err)
		assert.Equal(t, "test-policy", config.PolicyName)
		assert.Equal(t, "{\"Version\": \"2012-10-17\", \"Statement\": []}", config.PolicyDocument)
		assert.Equal(t, "Test policy", config.Description)
		assert.Equal(t, "/test/", config.Path)
		assert.Equal(t, map[string]string{"env": "test"}, config.Tags)
	})

	t.Run("valid configuration with minimal fields", func(t *testing.T) {
		rawConfig := json.RawMessage(`{
			"policyName": "minimal-policy",
			"policyDocument": "{\"Version\": \"2012-10-17\"}"
		}`)

		config, err := resolveIamPolicyConfig(ctx, rawConfig)
		require.NoError(t, err)
		assert.Equal(t, "minimal-policy", config.PolicyName)
		assert.Equal(t, "{\"Version\": \"2012-10-17\"}", config.PolicyDocument)
		assert.Equal(t, "/", config.Path) // default
		assert.Empty(t, config.Description)
		assert.Empty(t, config.Tags)
	})

	t.Run("missing policy name", func(t *testing.T) {
		rawConfig := json.RawMessage(`{
			"policyDocument": "{\"Version\": \"2012-10-17\"}"
		}`)

		_, err := resolveIamPolicyConfig(ctx, rawConfig)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "policyName is required")
	})

	t.Run("missing policy document", func(t *testing.T) {
		rawConfig := json.RawMessage(`{
			"policyName": "test-policy"
		}`)

		_, err := resolveIamPolicyConfig(ctx, rawConfig)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "policyDocument is required")
	})

	t.Run("invalid JSON policy document", func(t *testing.T) {
		rawConfig := json.RawMessage(`{
			"policyName": "test-policy",
			"policyDocument": "not valid json"
		}`)

		_, err := resolveIamPolicyConfig(ctx, rawConfig)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be valid JSON")
	})

	t.Run("invalid config JSON", func(t *testing.T) {
		rawConfig := json.RawMessage(`invalid json`)

		_, err := resolveIamPolicyConfig(ctx, rawConfig)
		require.Error(t, err)
	})
}

func TestResolveIamPolicyStatus(t *testing.T) {
	ctx := context.Background()

	t.Run("empty status", func(t *testing.T) {
		status, err := resolveIamPolicyStatus(ctx, nil)
		require.NoError(t, err)
		assert.NotNil(t, status)
		assert.Empty(t, status.PolicyArn)
		assert.Empty(t, status.PolicyId)
		assert.Empty(t, status.PolicyName)
	})

	t.Run("existing status", func(t *testing.T) {
		rawStatus := json.RawMessage(`{
			"policyArn": "arn:aws:iam::123456789012:policy/test-policy",
			"policyId": "ANPAI23HZ27SI6FQMGNQ2",
			"policyName": "test-policy",
			"currentVersionId": "v1"
		}`)

		status, err := resolveIamPolicyStatus(ctx, rawStatus)
		require.NoError(t, err)
		assert.Equal(t, "arn:aws:iam::123456789012:policy/test-policy", status.PolicyArn)
		assert.Equal(t, "ANPAI23HZ27SI6FQMGNQ2", status.PolicyId)
		assert.Equal(t, "test-policy", status.PolicyName)
		assert.Equal(t, "v1", status.CurrentVersionId)
	})

	t.Run("invalid status JSON", func(t *testing.T) {
		rawStatus := json.RawMessage(`invalid json`)

		_, err := resolveIamPolicyStatus(ctx, rawStatus)
		require.Error(t, err)
	})
}

func TestApplyIamPolicyConfigDefaults(t *testing.T) {
	t.Run("applies default path", func(t *testing.T) {
		config := &IamPolicyConfig{
			PolicyName:     "test-policy",
			PolicyDocument: "{}",
		}

		err := applyIamPolicyConfigDefaults(config)
		require.NoError(t, err)
		assert.Equal(t, "/", config.Path)
	})

	t.Run("preserves existing path", func(t *testing.T) {
		config := &IamPolicyConfig{
			PolicyName:     "test-policy",
			PolicyDocument: "{}",
			Path:           "/custom/",
		}

		err := applyIamPolicyConfigDefaults(config)
		require.NoError(t, err)
		assert.Equal(t, "/custom/", config.Path)
	})
}

func TestIamPolicyOperationsFactory(t *testing.T) {
	ctx := context.Background()

	t.Run("creates operations with valid config", func(t *testing.T) {
		factory := NewIamPolicyOperationsFactory()
		require.NotNil(t, factory)

		rawConfig := json.RawMessage(`{
			"policyName": "test-policy",
			"policyDocument": "{\"Version\": \"2012-10-17\"}"
		}`)

		ops, err := factory.NewOperations(ctx, rawConfig, nil)
		require.NoError(t, err)
		require.NotNil(t, ops)

		// Verify it implements ComponentOperations interface
		iamOps, ok := ops.(*IamPolicyOperations)
		require.True(t, ok)
		assert.NotNil(t, iamOps.config)
		assert.NotNil(t, iamOps.status)
		assert.NotNil(t, iamOps.iamClient)
	})

	t.Run("fails with invalid config", func(t *testing.T) {
		factory := NewIamPolicyOperationsFactory()

		rawConfig := json.RawMessage(`{
			"policyName": "test-policy"
		}`)

		_, err := factory.NewOperations(ctx, rawConfig, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "policyDocument is required")
	})

	t.Run("fails with invalid status", func(t *testing.T) {
		factory := NewIamPolicyOperationsFactory()

		rawConfig := json.RawMessage(`{
			"policyName": "test-policy",
			"policyDocument": "{}"
		}`)
		rawStatus := json.RawMessage(`invalid json`)

		_, err := factory.NewOperations(ctx, rawConfig, rawStatus)
		require.Error(t, err)
	})
}
