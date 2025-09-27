/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rds

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rinswind/deployment-operator/handler/base"
)

const (
	// HandlerName is the identifier for this RDS handler
	HandlerName = "rds"

	ControllerName = "rds-component"
)

// RdsOperationsFactory implements the ComponentOperationsFactory interface for RDS deployments.
// This factory creates stateful RdsOperations instances with pre-parsed configuration,
// eliminating repeated configuration parsing during reconciliation loops.
type RdsOperationsFactory struct{}

// CreateOperations creates a new stateful RdsOperations instance with pre-parsed configuration.
// This method is called once per reconciliation loop to eliminate repeated configuration parsing.
//
// The returned RdsOperations instance maintains the parsed configuration and can be used
// throughout the reconciliation loop without re-parsing the same configuration multiple times.
func (f *RdsOperationsFactory) CreateOperations(ctx context.Context, config json.RawMessage) (base.ComponentOperations, error) {
	// Parse configuration once for this reconciliation loop
	rdsConfig, err := parseRdsConfigFromRaw(config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse rds configuration: %w", err)
	}

	// Return stateful operations instance with pre-parsed configuration
	return &RdsOperations{
		config: rdsConfig,
	}, nil
}

// RdsOperations implements the ComponentOperations interface for RDS-based deployments.
// This struct provides all RDS-specific deployment, upgrade, and deletion operations
// for managing AWS RDS instances through the AWS SDK with pre-parsed configuration.
//
// This is a stateful operations instance created by RdsOperationsFactory that eliminates
// repeated configuration parsing by maintaining parsed configuration state.
type RdsOperations struct {
	// config holds the pre-parsed RDS configuration for this reconciliation loop
	config *RdsConfig

	// TODO: Add AWS SDK clients when implementing actual RDS operations
	// For example:
	// - rdsClient *rds.Client
	// - region string
	// - credentials aws.CredentialsProvider
}

// NewRdsOperationsFactory creates a new RdsOperationsFactory instance
func NewRdsOperationsFactory() *RdsOperationsFactory {
	return &RdsOperationsFactory{}
}

// NewRdsOperationsConfig creates a ComponentHandlerConfig for RDS with appropriate settings
func NewRdsOperationsConfig() base.ComponentHandlerConfig {
	config := base.DefaultComponentHandlerConfig(HandlerName, ControllerName)

	// RDS operations typically take longer than Helm operations
	// Adjust timeouts to account for database creation/modification times
	config.DefaultRequeue = 30 * time.Second     // RDS operations are slower
	config.StatusCheckRequeue = 15 * time.Second // Check database status less frequently
	config.ErrorRequeue = 30 * time.Second       // Give more time for transient errors

	return config
}

// parseRdsConfigFromRaw parses raw JSON configuration into RdsConfig struct
// This replaces any existing resolveRdsConfig function and works with raw JSON instead of Component
func parseRdsConfigFromRaw(rawConfig json.RawMessage) (*RdsConfig, error) {
	if len(rawConfig) == 0 {
		return nil, fmt.Errorf("rds configuration is required but not provided")
	}

	var config RdsConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal rds configuration: %w", err)
	}

	// TODO: Add RDS-specific configuration validation and defaults when implementing
	// For example:
	// - Validate AWS region
	// - Validate database engine
	// - Apply timeout defaults
	// - Validate instance class

	return &config, nil
}

// RdsConfig represents the configuration structure for RDS components
// that gets unmarshaled from Component.Spec.Config
type RdsConfig struct {
	// TODO: Define RDS-specific configuration fields when implementing
	// For example:
	// DatabaseEngine string `json:"databaseEngine" validate:"required"`
	// InstanceClass  string `json:"instanceClass" validate:"required"`
	// Region         string `json:"region" validate:"required"`
	// DatabaseName   string `json:"databaseName" validate:"required"`

	// Placeholder for now - actual implementation will define specific fields
	DatabaseName string `json:"databaseName,omitempty"`
}
