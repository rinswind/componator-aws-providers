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

// config.go contains RDS configuration parsing and status logic.
// This includes the RdsConfig struct definition and related parsing functions
// that handle Component.Spec.Config unmarshaling for RDS components.

package rds

import (
	"context"
	"encoding/json"
	"fmt"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// RdsConfig represents the configuration structure for RDS components
// that gets unmarshaled from Component.Spec.Config
type RdsConfig struct {
	// Instance Configuration - Required
	InstanceID string `json:"instanceID"`

	// Core Database Configuration
	DatabaseEngine string `json:"databaseEngine"`
	EngineVersion  string `json:"engineVersion"`
	InstanceClass  string `json:"instanceClass"`
	DatabaseName   string `json:"databaseName"`

	// AWS Configuration
	Region string `json:"region"`

	// Storage Configuration
	AllocatedStorage int32  `json:"allocatedStorage"`
	StorageType      string `json:"storageType,omitempty"`
	StorageEncrypted *bool  `json:"storageEncrypted,omitempty"`
	KmsKeyId         string `json:"kmsKeyId,omitempty"`

	// Database Credentials
	MasterUsername string `json:"masterUsername"`
	MasterPassword string `json:"masterPassword"`

	// Networking Configuration
	VpcSecurityGroupIds []string `json:"vpcSecurityGroupIds,omitempty"`
	SubnetGroupName     string   `json:"subnetGroupName,omitempty"`
	PubliclyAccessible  *bool    `json:"publiclyAccessible,omitempty"`
	Port                *int32   `json:"port,omitempty"`

	// Backup Configuration
	BackupRetentionPeriod *int32 `json:"backupRetentionPeriod,omitempty"`
	PreferredBackupWindow string `json:"preferredBackupWindow,omitempty"`

	// Maintenance Configuration
	PreferredMaintenanceWindow string `json:"preferredMaintenanceWindow,omitempty"`
	AutoMinorVersionUpgrade    *bool  `json:"autoMinorVersionUpgrade,omitempty"`

	// Performance Configuration
	MultiAZ                    *bool  `json:"multiAZ,omitempty"`
	PerformanceInsightsEnabled *bool  `json:"performanceInsightsEnabled,omitempty"`
	MonitoringInterval         *int32 `json:"monitoringInterval,omitempty"`

	// Deletion Protection
	DeletionProtection        *bool  `json:"deletionProtection,omitempty"`
	SkipFinalSnapshot         *bool  `json:"skipFinalSnapshot,omitempty"`
	FinalDBSnapshotIdentifier string `json:"finalDBSnapshotIdentifier,omitempty"`
}

// RdsStatus contains handler-specific status data for RDS deployments.
// This data is persisted across reconciliation loops in Component.Status.HandlerStatus.
type RdsStatus struct {
	// Instance identification and state
	InstanceStatus string `json:"instanceStatus,omitempty"`
	InstanceARN    string `json:"instanceARN,omitempty"`

	// Network information
	Endpoint         string `json:"endpoint,omitempty"`
	Port             int32  `json:"port,omitempty"`
	AvailabilityZone string `json:"availabilityZone,omitempty"`
}

// resolveRdsConfig unmarshals Component.Spec.Config into RdsConfig struct
// and applies sensible defaults for optional fields
func resolveRdsConfig(ctx context.Context, rawConfig json.RawMessage) (*RdsConfig, error) {
	var config RdsConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to parse rds config: %w", err)
	}

	// Validate required fields
	if config.InstanceID == "" {
		return nil, fmt.Errorf("InstanceID is required and cannot be empty")
	}

	// Apply defaults for optional fields
	if err := applyRdsConfigDefaults(&config); err != nil {
		return nil, fmt.Errorf("failed to apply configuration defaults: %w", err)
	}

	log := logf.FromContext(ctx)
	log.V(1).Info("Resolved rds config",
		"instanceIdentifier", config.InstanceID,
		"region", config.Region,
		"databaseEngine", config.DatabaseEngine,
		"instanceClass", config.InstanceClass,
		"databaseName", config.DatabaseName)

	return &config, nil
}

func resolveRdsStatus(ctx context.Context, rawStatus json.RawMessage) (*RdsStatus, error) {
	status := &RdsStatus{}
	if len(rawStatus) == 0 {
		logf.FromContext(ctx).V(1).Info("No existing rds status found, starting with empty status")
		return status, nil
	}

	if err := json.Unmarshal(rawStatus, &status); err != nil {
		return nil, err
	}

	return status, nil
}

// applyRdsConfigDefaults sets sensible defaults for optional RDS configuration fields
func applyRdsConfigDefaults(config *RdsConfig) error {
	// Storage defaults
	if config.StorageType == "" {
		config.StorageType = "gp2" // General Purpose SSD
	}
	if config.StorageEncrypted == nil {
		defaultEncrypted := true
		config.StorageEncrypted = &defaultEncrypted
	}

	// Network defaults
	if config.PubliclyAccessible == nil {
		defaultPublicAccess := false
		config.PubliclyAccessible = &defaultPublicAccess
	}

	// Backup defaults
	if config.BackupRetentionPeriod == nil {
		defaultRetention := int32(7) // 7 days
		config.BackupRetentionPeriod = &defaultRetention
	}

	// Maintenance defaults
	if config.AutoMinorVersionUpgrade == nil {
		defaultAutoUpgrade := true
		config.AutoMinorVersionUpgrade = &defaultAutoUpgrade
	}

	// Performance defaults
	if config.MultiAZ == nil {
		defaultMultiAZ := false // Single AZ by default for cost efficiency
		config.MultiAZ = &defaultMultiAZ
	}
	if config.PerformanceInsightsEnabled == nil {
		defaultPerfInsights := false
		config.PerformanceInsightsEnabled = &defaultPerfInsights
	}
	if config.MonitoringInterval == nil {
		defaultMonitoring := int32(0) // Disabled by default
		config.MonitoringInterval = &defaultMonitoring
	}

	// Deletion defaults
	if config.DeletionProtection == nil {
		defaultDeletionProtection := true // Enable by default for safety
		config.DeletionProtection = &defaultDeletionProtection
	}
	if config.SkipFinalSnapshot == nil {
		defaultSkipSnapshot := false // Take final snapshot by default
		config.SkipFinalSnapshot = &defaultSkipSnapshot
	}

	return nil
}
