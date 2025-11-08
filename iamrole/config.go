// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

// config.go contains IAM role configuration parsing and status logic.
// This includes the IamRoleConfig struct definition and related parsing functions
// that handle Component.Spec.Config unmarshaling for IAM role components.

package iamrole

import (
	"encoding/json"
	"fmt"
)

// IamRoleConfig represents the configuration structure for IAM role components
// that gets unmarshaled from Component.Spec.Config
type IamRoleConfig struct {
	// RoleName is the name of the IAM role to create/update
	RoleName string `json:"roleName"`

	// AssumeRolePolicy is the trust policy JSON document that defines which entities can assume the role
	AssumeRolePolicy json.RawMessage `json:"assumeRolePolicy"`

	// Description is an optional description for the role
	Description string `json:"description,omitempty"`

	// MaxSessionDuration is the maximum session duration in seconds (3600-43200)
	MaxSessionDuration int32 `json:"maxSessionDuration,omitempty"`

	// Path is the path for the role (defaults to "/")
	Path string `json:"path,omitempty"`

	// ManagedPolicyArns is the list of managed policy ARNs to attach to the role
	ManagedPolicyArns []string `json:"managedPolicyArns"`

	// Tags are optional key-value pairs to tag the IAM role
	Tags map[string]string `json:"tags,omitempty"`
}

// IamRoleStatus contains handler-specific status data for IAM role deployments.
// This data is persisted across reconciliation loops in Component.Status.ProviderStatus.
type IamRoleStatus struct {
	RoleArn          string   `json:"roleArn,omitempty"`
	RoleId           string   `json:"roleId,omitempty"`
	RoleName         string   `json:"roleName,omitempty"`
	AttachedPolicies []string `json:"attachedPolicies,omitempty"`
}

// resolveSpec validates config and applies defaults
func resolveSpec(config *IamRoleConfig) error {
	// Validate required fields
	if config.RoleName == "" {
		return fmt.Errorf("roleName is required and cannot be empty")
	}
	if len(config.AssumeRolePolicy) == 0 {
		return fmt.Errorf("assumeRolePolicy is required and cannot be empty")
	}
	if len(config.ManagedPolicyArns) == 0 {
		return fmt.Errorf("managedPolicyArns is required and must contain at least one policy ARN")
	}

	// Validate assumeRolePolicy is valid JSON
	if !json.Valid(config.AssumeRolePolicy) {
		return fmt.Errorf("assumeRolePolicy must be valid JSON")
	}

	// Note: We don't validate maxSessionDuration range - let AWS enforce current limits
	// AWS limits change over time and hardcoding them creates maintenance burden

	// Apply defaults
	if err := applyDefaults(config); err != nil {
		return err
	}

	return nil
}

// applyDefaults sets sensible defaults for optional IAM role configuration fields
func applyDefaults(config *IamRoleConfig) error {
	// Default path to root if not specified
	if config.Path == "" {
		config.Path = "/"
	}

	// Default maxSessionDuration to 1 hour if not specified
	if config.MaxSessionDuration == 0 {
		config.MaxSessionDuration = 3600
	}

	return nil
}
