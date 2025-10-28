// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

// config.go contains IAM role configuration parsing and status logic.
// This includes the IamRoleConfig struct definition and related parsing functions
// that handle Component.Spec.Config unmarshaling for IAM role components.

package iamrole

import (
	"context"
	"encoding/json"
	"fmt"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// IamRoleConfig represents the configuration structure for IAM role components
// that gets unmarshaled from Component.Spec.Config
type IamRoleConfig struct {
	// RoleName is the name of the IAM role to create/update
	RoleName string `json:"roleName"`

	// AssumeRolePolicy is the trust policy JSON document that defines which entities can assume the role
	AssumeRolePolicy string `json:"assumeRolePolicy"`

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
	// RoleArn is the AWS ARN of the created IAM role
	RoleArn string `json:"roleArn,omitempty"`

	// RoleId is the AWS-assigned unique identifier for the role
	RoleId string `json:"roleId,omitempty"`

	// RoleName is the name of the role as created in AWS
	RoleName string `json:"roleName,omitempty"`

	// AttachedPolicies is the list of managed policy ARNs currently attached to the role
	AttachedPolicies []string `json:"attachedPolicies,omitempty"`
}

// resolveIamRoleConfig unmarshals Component.Spec.Config into IamRoleConfig struct
// and applies sensible defaults for optional fields
func resolveIamRoleConfig(ctx context.Context, rawConfig json.RawMessage) (*IamRoleConfig, error) {
	var config IamRoleConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to parse iam-role config: %w", err)
	}

	// Validate required fields
	if config.RoleName == "" {
		return nil, fmt.Errorf("roleName is required and cannot be empty")
	}
	if config.AssumeRolePolicy == "" {
		return nil, fmt.Errorf("assumeRolePolicy is required and cannot be empty")
	}
	if len(config.ManagedPolicyArns) == 0 {
		return nil, fmt.Errorf("managedPolicyArns is required and must contain at least one policy ARN")
	}

	// Validate assumeRolePolicy is valid JSON
	if !json.Valid([]byte(config.AssumeRolePolicy)) {
		return nil, fmt.Errorf("assumeRolePolicy must be valid JSON")
	}

	// Note: We don't validate maxSessionDuration range - let AWS enforce current limits
	// AWS limits change over time and hardcoding them creates maintenance burden

	// Apply defaults for optional fields
	if err := applyIamRoleConfigDefaults(&config); err != nil {
		return nil, fmt.Errorf("failed to apply configuration defaults: %w", err)
	}

	log := logf.FromContext(ctx)
	log.V(1).Info("Resolved iam-role config",
		"roleName", config.RoleName,
		"path", config.Path,
		"maxSessionDuration", config.MaxSessionDuration,
		"hasDescription", config.Description != "",
		"policyCount", len(config.ManagedPolicyArns),
		"tagCount", len(config.Tags))

	return &config, nil
}

// resolveIamRoleStatus unmarshals existing handler status or returns empty status
func resolveIamRoleStatus(ctx context.Context, rawStatus json.RawMessage) (*IamRoleStatus, error) {
	status := &IamRoleStatus{}
	if len(rawStatus) == 0 {
		logf.FromContext(ctx).V(1).Info("No existing iam-role status found, starting with empty status")
		return status, nil
	}

	if err := json.Unmarshal(rawStatus, &status); err != nil {
		return nil, fmt.Errorf("failed to parse iam-role status: %w", err)
	}

	return status, nil
}

// applyIamRoleConfigDefaults sets sensible defaults for optional IAM role configuration fields
func applyIamRoleConfigDefaults(config *IamRoleConfig) error {
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
