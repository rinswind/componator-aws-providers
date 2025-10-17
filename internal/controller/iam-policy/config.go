// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

// config.go contains IAM policy configuration parsing and status logic.
// This includes the IamPolicyConfig struct definition and related parsing functions
// that handle Component.Spec.Config unmarshaling for IAM policy components.

package iampolicy

import (
	"context"
	"encoding/json"
	"fmt"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// IamPolicyConfig represents the configuration structure for IAM policy components
// that gets unmarshaled from Component.Spec.Config
type IamPolicyConfig struct {
	// PolicyName is the name of the IAM policy to create/update
	PolicyName string `json:"policyName"`

	// PolicyDocument is the JSON policy document following AWS IAM policy syntax
	PolicyDocument string `json:"policyDocument"`

	// Description is an optional description for the policy
	Description string `json:"description,omitempty"`

	// Path is the path for the policy (defaults to "/")
	Path string `json:"path,omitempty"`

	// Tags are optional key-value pairs to tag the IAM policy
	Tags map[string]string `json:"tags,omitempty"`

	// Region is the AWS region for IAM operations (IAM is global but API endpoints are regional)
	Region string `json:"region,omitempty"`
}

// IamPolicyStatus contains handler-specific status data for IAM policy deployments.
// This data is persisted across reconciliation loops in Component.Status.HandlerStatus.
type IamPolicyStatus struct {
	// PolicyArn is the AWS ARN of the created IAM policy
	PolicyArn string `json:"policyArn,omitempty"`

	// PolicyId is the AWS-assigned unique identifier for the policy
	PolicyId string `json:"policyId,omitempty"`

	// PolicyName is the name of the policy as created in AWS
	PolicyName string `json:"policyName,omitempty"`

	// CurrentVersionId is the version ID of the current default policy version
	CurrentVersionId string `json:"currentVersionId,omitempty"`
}

// resolveIamPolicyConfig unmarshals Component.Spec.Config into IamPolicyConfig struct
// and applies sensible defaults for optional fields
func resolveIamPolicyConfig(ctx context.Context, rawConfig json.RawMessage) (*IamPolicyConfig, error) {
	var config IamPolicyConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to parse iam-policy config: %w", err)
	}

	// Validate required fields
	if config.PolicyName == "" {
		return nil, fmt.Errorf("policyName is required and cannot be empty")
	}
	if config.PolicyDocument == "" {
		return nil, fmt.Errorf("policyDocument is required and cannot be empty")
	}

	// Validate policyDocument is valid JSON
	if !json.Valid([]byte(config.PolicyDocument)) {
		return nil, fmt.Errorf("policyDocument must be valid JSON")
	}

	// Apply defaults for optional fields
	if err := applyIamPolicyConfigDefaults(&config); err != nil {
		return nil, fmt.Errorf("failed to apply configuration defaults: %w", err)
	}

	log := logf.FromContext(ctx)
	log.V(1).Info("Resolved iam-policy config",
		"policyName", config.PolicyName,
		"path", config.Path,
		"region", config.Region,
		"hasDescription", config.Description != "",
		"tagCount", len(config.Tags))

	return &config, nil
}

// resolveIamPolicyStatus unmarshals existing handler status or returns empty status
func resolveIamPolicyStatus(ctx context.Context, rawStatus json.RawMessage) (*IamPolicyStatus, error) {
	status := &IamPolicyStatus{}
	if len(rawStatus) == 0 {
		logf.FromContext(ctx).V(1).Info("No existing iam-policy status found, starting with empty status")
		return status, nil
	}

	if err := json.Unmarshal(rawStatus, &status); err != nil {
		return nil, fmt.Errorf("failed to parse iam-policy status: %w", err)
	}

	return status, nil
}

// applyIamPolicyConfigDefaults sets sensible defaults for optional IAM policy configuration fields
func applyIamPolicyConfigDefaults(config *IamPolicyConfig) error {
	// Default path to root if not specified
	if config.Path == "" {
		config.Path = "/"
	}

	// Default region to us-east-1 (IAM is global but uses regional endpoints)
	if config.Region == "" {
		config.Region = "us-east-1"
	}

	return nil
}
