// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

// config.go contains config-reader configuration parsing and validation logic.

package configreader

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-playground/validator/v10"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// configReaderConfigValidator is a package-level validator instance that is reused across all reconciliations.
// validator.Validator is thread-safe and designed for concurrent use, making this safe to share.
var configReaderConfigValidator = validator.New()

// ConfigReaderConfig represents the configuration structure for config-reader components
// that gets unmarshaled from Component.Spec.Config.
type ConfigReaderConfig struct {
	// Sources specifies the ConfigMaps to read from
	Sources []ConfigMapSource `json:"sources" validate:"required,min=1,dive"`
}

// ConfigMapSource defines a single ConfigMap source with export mappings
type ConfigMapSource struct {
	// Name is the ConfigMap name
	Name string `json:"name" validate:"required"`

	// Namespace is the ConfigMap namespace
	Namespace string `json:"namespace" validate:"required"`

	// Exports defines which keys to extract and optionally rename
	Exports []ExportMapping `json:"exports" validate:"required,min=1,dive"`
}

// ExportMapping defines a key export with optional renaming
type ExportMapping struct {
	// Key is the ConfigMap data key to export
	Key string `json:"key" validate:"required"`

	// As is the optional output name (defaults to Key if not specified)
	As string `json:"as,omitempty"`
}

// ConfigReaderStatus contains handler-specific status data for config-reader components.
// This is a simple map of exported values that gets persisted in Component.Status.HandlerStatus.
type ConfigReaderStatus map[string]string

// resolveConfigReaderConfig unmarshals and validates Component.Spec.Config into ConfigReaderConfig struct.
func resolveConfigReaderConfig(ctx context.Context, rawConfig json.RawMessage) (*ConfigReaderConfig, error) {
	log := logf.FromContext(ctx)

	var config ConfigReaderConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config-reader config: %w", err)
	}

	// Validate configuration using shared validator instance
	if err := configReaderConfigValidator.Struct(&config); err != nil {
		return nil, fmt.Errorf("config-reader config validation failed: %w", err)
	}

	log.V(1).Info("Resolved config-reader config",
		"sourceCount", len(config.Sources))

	return &config, nil
}

// resolveConfigReaderStatus unmarshals Component.Status.HandlerStatus into ConfigReaderStatus.
func resolveConfigReaderStatus(ctx context.Context, rawStatus json.RawMessage) (ConfigReaderStatus, error) {
	status := make(ConfigReaderStatus)
	if len(rawStatus) == 0 {
		logf.FromContext(ctx).V(1).Info("No existing config-reader status found, starting with empty status")
		return status, nil
	}

	if err := json.Unmarshal(rawStatus, &status); err != nil {
		return nil, fmt.Errorf("failed to parse config-reader status: %w", err)
	}

	return status, nil
}
