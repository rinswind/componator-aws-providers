// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package rds

import (
	"fmt"
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

	// Storage Configuration
	AllocatedStorage int32  `json:"allocatedStorage"`
	StorageType      string `json:"storageType,omitempty"`
	StorageEncrypted *bool  `json:"storageEncrypted,omitempty"`
	KmsKeyId         string `json:"kmsKeyId,omitempty"`

	// Database Credentials
	MasterUsername           string `json:"masterUsername"`
	ManageMasterUserPassword *bool  `json:"manageMasterUserPassword,omitempty"`
	MasterUserSecretKmsKeyId string `json:"masterUserSecretKmsKeyId,omitempty"`

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
// This data is persisted across reconciliation loops in Component.Status.ProviderStatus.
type RdsStatus struct {
	// Instance identification and state
	InstanceStatus string `json:"instanceStatus,omitempty"`
	InstanceARN    string `json:"instanceARN,omitempty"`

	// Network information
	Endpoint         string `json:"endpoint,omitempty"`
	Port             int32  `json:"port,omitempty"`
	AvailabilityZone string `json:"availabilityZone,omitempty"`

	// Credentials information
	MasterUserSecretArn string `json:"masterUserSecretArn,omitempty"`
}

// resolveSpec validates config and applies defaults
func resolveSpec(config *RdsConfig) error {
	// Validate required fields
	if config.InstanceID == "" {
		return fmt.Errorf("InstanceID is required and cannot be empty")
	}

	// Apply defaults
	if err := applyDefaults(config); err != nil {
		return err
	}

	return nil
}

// applyDefaults sets sensible defaults for optional RDS configuration fields
func applyDefaults(config *RdsConfig) error {
	// Validate required credentials fields
	if config.MasterUsername == "" {
		return fmt.Errorf("masterUsername is required and cannot be empty")
	}

	// Always use RDS-managed passwords - enforce this policy
	if config.ManageMasterUserPassword == nil {
		defaultManaged := true
		config.ManageMasterUserPassword = &defaultManaged
	}
	if !*config.ManageMasterUserPassword {
		return fmt.Errorf("manageMasterUserPassword must be true - explicit password management is not supported. AWS RDS will generate secure passwords automatically")
	}

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
