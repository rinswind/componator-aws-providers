// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package iamrole

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveSpec(t *testing.T) {
	t.Run("missing roleName", func(t *testing.T) {
		config := &IamRoleConfig{
			AssumeRolePolicy:  "{\"Version\":\"2012-10-17\"}",
			ManagedPolicyArns: []string{"arn:aws:iam::123456789012:policy/test"},
		}

		err := resolveSpec(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "roleName is required")
	})

	t.Run("missing assumeRolePolicy", func(t *testing.T) {
		config := &IamRoleConfig{
			RoleName:          "test-role",
			ManagedPolicyArns: []string{"arn:aws:iam::123456789012:policy/test"},
		}

		err := resolveSpec(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "assumeRolePolicy is required")
	})

	t.Run("empty managedPolicyArns", func(t *testing.T) {
		config := &IamRoleConfig{
			RoleName:          "test-role",
			AssumeRolePolicy:  "{\"Version\":\"2012-10-17\"}",
			ManagedPolicyArns: []string{},
		}

		err := resolveSpec(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "managedPolicyArns is required")
	})

	t.Run("invalid assumeRolePolicy JSON", func(t *testing.T) {
		config := &IamRoleConfig{
			RoleName:          "test-role",
			AssumeRolePolicy:  "not valid json",
			ManagedPolicyArns: []string{"arn:aws:iam::123456789012:policy/test"},
		}

		err := resolveSpec(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "assumeRolePolicy must be valid JSON")
	})
}

func TestApplyDefaults(t *testing.T) {
	t.Run("apply path default", func(t *testing.T) {
		config := &IamRoleConfig{
			RoleName: "test",
		}

		err := applyDefaults(config)
		require.NoError(t, err)
		assert.Equal(t, "/", config.Path)
	})

	t.Run("apply maxSessionDuration default", func(t *testing.T) {
		config := &IamRoleConfig{
			RoleName: "test",
		}

		err := applyDefaults(config)
		require.NoError(t, err)
		assert.Equal(t, int32(3600), config.MaxSessionDuration)
	})

	t.Run("preserve existing values", func(t *testing.T) {
		config := &IamRoleConfig{
			RoleName:           "test",
			Path:               "/custom/",
			MaxSessionDuration: 7200,
		}

		err := applyDefaults(config)
		require.NoError(t, err)
		assert.Equal(t, "/custom/", config.Path)
		assert.Equal(t, int32(7200), config.MaxSessionDuration)
	})
}
