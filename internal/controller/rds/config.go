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
	"time"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// RdsConfig represents the configuration structure for RDS components
// that gets unmarshaled from Component.Spec.Config
type RdsConfig struct {
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

	// Timeouts for operations
	Timeouts *RdsTimeouts `json:"timeouts,omitempty"`
}

// RdsTimeouts represents timeout configuration for RDS operations
type RdsTimeouts struct {
	// Create timeout - how long to wait for RDS instance creation
	Create *Duration `json:"create,omitempty"`

	// Update timeout - how long to wait for RDS instance modifications
	Update *Duration `json:"update,omitempty"`

	// Delete timeout - how long to wait for RDS instance deletion
	Delete *Duration `json:"delete,omitempty"`
}

// RdsStatus contains handler-specific status data for RDS deployments.
// This data is persisted across reconciliation loops in Component.Status.HandlerStatus.
type RdsStatus struct {
	// Instance identification and state
	InstanceID     string `json:"instanceId,omitempty"`
	InstanceStatus string `json:"instanceStatus,omitempty"`
	DatabaseName   string `json:"databaseName,omitempty"`

	// Timing information
	CreatedAt        string `json:"createdAt,omitempty"`
	LastModifiedTime string `json:"lastModifiedTime,omitempty"`

	// Deployment metadata
	EngineVersion    string `json:"engineVersion,omitempty"`
	InstanceClass    string `json:"instanceClass,omitempty"`
	AllocatedStorage int32  `json:"allocatedStorage,omitempty"`

	// Network information
	Endpoint string `json:"endpoint,omitempty"`
	Port     int32  `json:"port,omitempty"`

	// Operational status
	BackupRetentionPeriod int32 `json:"backupRetentionPeriod,omitempty"`
	MultiAZ               bool  `json:"multiAZ,omitempty"`

	// Error information
	LastError     string `json:"lastError,omitempty"`
	LastErrorTime string `json:"lastErrorTime,omitempty"`
}

// Duration wraps time.Duration with JSON marshaling support
type Duration struct {
	time.Duration
}

// UnmarshalJSON implements json.Unmarshaler interface for Duration
func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	dur, err := time.ParseDuration(s)
	if err != nil {
		return err
	}

	d.Duration = dur
	return nil
}

// resolveRdsConfig unmarshals Component.Spec.Config into RdsConfig struct
// and applies sensible defaults for optional fields
func resolveRdsConfig(ctx context.Context, rawConfig json.RawMessage) (*RdsConfig, error) {
	var config RdsConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to parse rds config: %w", err)
	}

	// Apply defaults for optional fields
	if err := applyRdsConfigDefaults(&config); err != nil {
		return nil, fmt.Errorf("failed to apply configuration defaults: %w", err)
	}

	log := logf.FromContext(ctx)
	log.V(1).Info("Resolved rds config",
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

	// Set default port based on engine
	if config.Port == nil {
		var defaultPort int32
		switch config.DatabaseEngine {
		case "postgres":
			defaultPort = 5432
		case "mysql":
			defaultPort = 3306
		default:
			// Let AWS validate the engine - don't fail here
			defaultPort = 5432 // Default to postgres port
		}
		config.Port = &defaultPort
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

	// Timeout defaults
	if config.Timeouts == nil {
		config.Timeouts = &RdsTimeouts{}
	}
	if config.Timeouts.Create == nil {
		config.Timeouts.Create = &Duration{Duration: 40 * time.Minute} // RDS creation can take 20-40 minutes
	}
	if config.Timeouts.Update == nil {
		config.Timeouts.Update = &Duration{Duration: 80 * time.Minute} // Updates can take longer
	}
	if config.Timeouts.Delete == nil {
		config.Timeouts.Delete = &Duration{Duration: 60 * time.Minute} // Deletion with final snapshot
	}

	return nil
}
