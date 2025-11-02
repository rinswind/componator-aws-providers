// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package secretpush

import "fmt"

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

// SecretPushSpec represents the configuration structure for secret-push components
// that gets unmarshaled from Component.Spec.Config
type SecretPushSpec struct {
	// SecretName is the name/path of the secret in AWS Secrets Manager
	SecretName string `json:"secretName"`

	// KmsKeyId is optional KMS key for encryption
	KmsKeyId string `json:"kmsKeyId,omitempty"`

	// Fields is a flat map of field definitions
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
	Value     string         `json:"value,omitempty"`
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
	SecretArn    string `json:"secretArn,omitempty"`
	SecretName   string `json:"secretName,omitempty"`
	VersionId    string `json:"versionId,omitempty"`
	Region       string `json:"region,omitempty"`
	LastSyncTime string `json:"lastSyncTime,omitempty"`
	FieldCount   int    `json:"fieldCount,omitempty"`
}

// resolveSpec validates config and applies defaults in-place
func resolveSpec(config *SecretPushSpec) error {
	// Validate required fields
	if config.SecretName == "" {
		return fmt.Errorf("secretName is required and cannot be empty")
	}
	if len(config.Fields) == 0 {
		return fmt.Errorf("at least one field is required")
	}

	// Validate each field has EXACTLY ONE of value or generator
	for fieldName, fieldSpec := range config.Fields {
		hasValue := fieldSpec.Value != ""
		hasGenerator := fieldSpec.Generator != nil

		if hasValue && hasGenerator {
			return fmt.Errorf("field %s: cannot have both value and generator", fieldName)
		}
		if !hasValue && !hasGenerator {
			return fmt.Errorf("field %s: must have either value or generator", fieldName)
		}

		// Validate generator config
		if hasGenerator {
			if fieldSpec.Generator.PasswordLength <= 0 {
				return fmt.Errorf("field %s: passwordLength must be positive", fieldName)
			}
			if fieldSpec.Generator.PasswordLength > 4096 {
				return fmt.Errorf("field %s: passwordLength cannot exceed 4096 (AWS limit)", fieldName)
			}
		}
	}

	// Apply defaults and validate policy fields
	var err error
	config.UpdatePolicy, err = resolveEnumValue(
		config.UpdatePolicy, "updatePolicy", DefaultUpdatePolicy, UpdatePolicyIfNotExists, UpdatePolicyAlwaysUpdate)
	if err != nil {
		return err
	}

	config.DeletionPolicy, err = resolveEnumValue(
		config.DeletionPolicy, "deletionPolicy", DefaultDeletionPolicy, DeletionPolicyDelete, DeletionPolicyRetain)
	if err != nil {
		return err
	}

	return nil
}

// resolveEnumValue validates a string field against allowed values and applies default if empty
func resolveEnumValue(value, fieldName string, defaultValue string, validValues ...string) (string, error) {
	if value == "" {
		return defaultValue, nil
	}
	for _, valid := range validValues {
		if value == valid {
			return value, nil
		}
	}
	return "", fmt.Errorf("%s must be one of %v, got: %s", fieldName, validValues, value)
}
