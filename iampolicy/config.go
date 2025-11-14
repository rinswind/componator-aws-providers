// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

// config.go contains IAM policy configuration parsing and status logic.
// This includes the IamPolicyConfig struct definition and related parsing functions
// that handle Component.Spec.Config unmarshaling for IAM policy components.

package iampolicy

import (
	"encoding/json"
	"fmt"
)

// IamPolicyConfig represents the configuration structure for IAM policy components
// that gets unmarshaled from Component.Spec.Config
type IamPolicyConfig struct {
	// PolicyName is the name of the IAM policy to create/update
	PolicyName string `json:"policyName"`

	// PolicyDocument is the JSON policy document following AWS IAM policy syntax
	// Must be a valid JSON string in AWS IAM format
	PolicyDocument string `json:"policyDocument"`

	// Description is an optional description for the policy
	Description string `json:"description,omitempty"`

	// Path is the path for the policy (defaults to "/")
	Path string `json:"path,omitempty"`

	// Tags are optional key-value pairs to tag the IAM policy
	Tags map[string]string `json:"tags,omitempty"`
}

// IamPolicyStatus contains handler-specific status data for IAM policy deployments.
// This data is persisted across reconciliation loops in Component.Status.ProviderStatus.
type IamPolicyStatus struct {
	PolicyArn        string `json:"policyArn,omitempty"`
	PolicyId         string `json:"policyId,omitempty"`
	PolicyName       string `json:"policyName,omitempty"`
	CurrentVersionId string `json:"currentVersionId,omitempty"`
}

// resolveSpec validates config and applies defaults
func resolveSpec(config *IamPolicyConfig) error {
	// Validate required fields
	if config.PolicyName == "" {
		return fmt.Errorf("policyName is required and cannot be empty")
	}
	if config.PolicyDocument == "" {
		return fmt.Errorf("policyDocument is required and cannot be empty")
	}

	// Validate policyDocument is valid JSON
	if !json.Valid([]byte(config.PolicyDocument)) {
		return fmt.Errorf("policyDocument must be valid JSON")
	}

	// Apply defaults
	if err := applyDefaults(config); err != nil {
		return err
	}

	return nil
}

// applyDefaults sets sensible defaults for optional IAM policy configuration fields
func applyDefaults(config *IamPolicyConfig) error {
	// Default path to root if not specified
	if config.Path == "" {
		config.Path = "/"
	}

	return nil
}
