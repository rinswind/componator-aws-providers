// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

// config.go contains secret-push configuration parsing and status logic.

package secretpush

import (
	"context"
	"encoding/json"
	"fmt"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// UpdatePolicy values
	UpdatePolicyIfNotExists  = "IfNotExists"
	UpdatePolicyAlwaysUpdate = "AlwaysUpdate"

	// DeletionPolicy values
	DeletionPolicyDelete = "Delete"
	DeletionPolicyRetain = "Retain"

	// Default values
	DefaultUpdatePolicy   = UpdatePolicyIfNotExists
	DefaultDeletionPolicy = DeletionPolicyDelete
)

// SecretPushConfig represents the configuration structure for secret-push components
// that gets unmarshaled from Component.Spec.Config
type SecretPushConfig struct {
	// SecretName is the name/path of the secret in AWS Secrets Manager
	SecretName string `json:"secretName"`

	// KmsKeyId is optional KMS key for encryption
	KmsKeyId string `json:"kmsKeyId,omitempty"`

	// Fields is a flat map of field definitions
	// Each field has EXACTLY ONE of Value or Generator (validated in factory)
	Fields map[string]FieldSpec `json:"fields"`

	// UpdatePolicy controls whether to update existing secrets
	// Valid values: UpdatePolicyIfNotExists (default), UpdatePolicyAlwaysUpdate
	UpdatePolicy string `json:"updatePolicy,omitempty"`

	// DeletionPolicy controls whether to delete secrets on Component deletion
	// Valid values: DeletionPolicyDelete (default), DeletionPolicyRetain
	DeletionPolicy string `json:"deletionPolicy,omitempty"`
}

// FieldSpec defines a single field in the secret
// Must have EXACTLY ONE of Value or Generator
type FieldSpec struct {
	// Value is a static value (supports cross-component refs, resolved by controller)
	Value string `json:"value,omitempty"`

	// Generator specifies AWS GetRandomPassword parameters
	Generator *GeneratorSpec `json:"generator,omitempty"`
}

// GeneratorSpec defines AWS GetRandomPassword parameters
type GeneratorSpec struct {
	PasswordLength          int64  `json:"passwordLength"`
	RequireEachIncludedType bool   `json:"requireEachIncludedType,omitempty"`
	ExcludePunctuation      bool   `json:"excludePunctuation,omitempty"`
	ExcludeNumbers          bool   `json:"excludeNumbers,omitempty"`
	ExcludeLowercase        bool   `json:"excludeLowercase,omitempty"`
	ExcludeUppercase        bool   `json:"excludeUppercase,omitempty"`
	IncludeSpace            bool   `json:"includeSpace,omitempty"`
	ExcludeCharacters       string `json:"excludeCharacters,omitempty"`
}

// SecretPushStatus contains handler-specific status data for secret-push deployments.
// This data is persisted across reconciliation loops in Component.Status.ProviderStatus.
type SecretPushStatus struct {
	// SecretArn is the AWS ARN of the created secret
	SecretArn string `json:"secretArn,omitempty"`

	// SecretName is the name/path in AWS Secrets Manager
	SecretName string `json:"secretName,omitempty"`

	// VersionId is the current secret version
	VersionId string `json:"versionId,omitempty"`

	// Region is the AWS region
	Region string `json:"region,omitempty"`

	// LastSyncTime is when the secret was last pushed
	LastSyncTime string `json:"lastSyncTime,omitempty"`

	// FieldCount is the number of fields in the secret
	FieldCount int `json:"fieldCount,omitempty"`
}

// resolveSecretPushConfig unmarshals Component.Spec.Config into SecretPushConfig struct
// and validates configuration including the mutual exclusivity of value and generator
func resolveSecretPushConfig(ctx context.Context, rawConfig json.RawMessage) (*SecretPushConfig, error) {
	var config SecretPushConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to parse secret-push config: %w", err)
	}

	// Validate required fields
	if config.SecretName == "" {
		return nil, fmt.Errorf("secretName is required and cannot be empty")
	}
	if len(config.Fields) == 0 {
		return nil, fmt.Errorf("at least one field is required")
	}

	// Validate each field has EXACTLY ONE of value or generator
	for fieldName, fieldSpec := range config.Fields {
		hasValue := fieldSpec.Value != ""
		hasGenerator := fieldSpec.Generator != nil

		if hasValue && hasGenerator {
			return nil, fmt.Errorf("field %s: cannot have both value and generator", fieldName)
		}
		if !hasValue && !hasGenerator {
			return nil, fmt.Errorf("field %s: must have either value or generator", fieldName)
		}

		// Validate generator config
		if hasGenerator {
			if err := validateGeneratorSpec(fieldName, fieldSpec.Generator); err != nil {
				return nil, err
			}
		}
	}

	// Apply defaults for optional fields
	if err := applySecretPushConfigDefaults(&config); err != nil {
		return nil, fmt.Errorf("failed to apply configuration defaults: %w", err)
	}

	log := logf.FromContext(ctx)
	log.V(1).Info("Resolved secret-push config",
		"secretName", config.SecretName,
		"fieldCount", len(config.Fields),
		"updatePolicy", config.UpdatePolicy,
		"deletionPolicy", config.DeletionPolicy)

	return &config, nil
}

// resolveSecretPushStatus unmarshals existing handler status or returns empty status
func resolveSecretPushStatus(ctx context.Context, rawStatus json.RawMessage) (*SecretPushStatus, error) {
	status := &SecretPushStatus{}
	if len(rawStatus) == 0 {
		logf.FromContext(ctx).V(1).Info("No existing secret-push status found, starting with empty status")
		return status, nil
	}

	if err := json.Unmarshal(rawStatus, &status); err != nil {
		return nil, fmt.Errorf("failed to parse secret-push status: %w", err)
	}

	return status, nil
}

// validateGeneratorSpec validates AWS GetRandomPassword parameters
func validateGeneratorSpec(fieldName string, gen *GeneratorSpec) error {
	if gen.PasswordLength <= 0 {
		return fmt.Errorf("field %s: passwordLength must be positive", fieldName)
	}
	if gen.PasswordLength > 4096 {
		return fmt.Errorf("field %s: passwordLength cannot exceed 4096 (AWS limit)", fieldName)
	}
	return nil
}

// applySecretPushConfigDefaults sets sensible defaults for optional configuration fields
func applySecretPushConfigDefaults(config *SecretPushConfig) error {
	// Region is optional - if empty, AWS SDK will use default config chain
	// (AWS_REGION env var, EC2 instance metadata, etc.)

	// Default update policy
	if config.UpdatePolicy == "" {
		config.UpdatePolicy = DefaultUpdatePolicy
	}

	// Validate update policy value
	if config.UpdatePolicy != UpdatePolicyIfNotExists && config.UpdatePolicy != UpdatePolicyAlwaysUpdate {
		return fmt.Errorf("updatePolicy must be '%s' or '%s', got: %s",
			UpdatePolicyIfNotExists, UpdatePolicyAlwaysUpdate, config.UpdatePolicy)
	}

	// Default deletion policy
	if config.DeletionPolicy == "" {
		config.DeletionPolicy = DefaultDeletionPolicy
	}

	// Validate deletion policy value
	if config.DeletionPolicy != DeletionPolicyDelete && config.DeletionPolicy != DeletionPolicyRetain {
		return fmt.Errorf("deletionPolicy must be '%s' or '%s', got: %s",
			DeletionPolicyDelete, DeletionPolicyRetain, config.DeletionPolicy)
	}

	return nil
}
