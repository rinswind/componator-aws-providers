// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package rds

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/rinswind/deployment-operator/componentkit/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Deploy handles all RDS-specific deployment operations using pre-parsed configuration
// Implements ComponentOperations.Deploy interface method.
func (r *RdsOperations) Deploy(ctx context.Context) (*controller.ActionResult, error) {
	instanceID := r.config.InstanceID

	log := logf.FromContext(ctx).WithValues("instanceId", instanceID)

	log.Info("Starting RDS deployment")

	// Check if the instance exists
	instance, err := r.getInstanceData(ctx, instanceID)
	if err != nil {
		checkErr := fmt.Errorf("failed to check RDS instance existence: %w", err)
		log.Error(checkErr, "Failed to check if RDS instance exists")
		return controller.ActionResultForError(r.status, checkErr, rdsErrorClassifier)
	}

	if instance != nil {
		log.Info("RDS instance exists, modifying existing instance")
		return r.modifyInstance(ctx)
	}

	log.Info("RDS instance does not exist, creating new instance")
	return r.createInstance(ctx)
}

// CheckDeployment verifies the current deployment status using pre-parsed configuration
// Implements ComponentOperations.CheckDeployment interface method.
func (r *RdsOperations) CheckDeployment(ctx context.Context) (*controller.CheckResult, error) {
	instanceID := r.config.InstanceID

	log := logf.FromContext(ctx).WithValues("instanceId", instanceID)

	log.Info("Checking RDS deployment status")

	// Query RDS instance status
	instance, err := r.getInstanceData(ctx, instanceID)
	if err != nil {
		describeErr := fmt.Errorf("failed to describe RDS instance: %w", err)
		log.Error(describeErr, "Failed to check RDS instance status")
		return controller.CheckResultForError(r.status, describeErr, rdsErrorClassifier)
	}

	if instance == nil {
		notFoundErr := fmt.Errorf("RDS instance %s not found during deployment check", instanceID)
		log.Error(notFoundErr, "RDS instance not found")
		return controller.CheckFailure(r.status, notFoundErr)
	}

	// Update status with current instance information
	r.updateStatus(instance)

	// Check if deployment is complete
	log = log.WithValues("status", r.status.InstanceStatus)

	switch r.status.InstanceStatus {
	case "available":
		log.Info("RDS instance deployment completed successfully",
			"endpoint", r.status.Endpoint,
			"port", r.status.Port)

		return controller.CheckComplete(r.status)

	case "creating", "backing-up", "modifying":
		// Still in progress
		log.Info("RDS instance deployment in progress")
		return controller.CheckInProgress(r.status)

	case "failed", "incompatible-restore", "incompatible-network":
		// Failed states
		failureErr := fmt.Errorf("RDS instance deployment failed with status: %s", r.status.InstanceStatus)
		log.Error(failureErr, "RDS instance deployment failed")
		return controller.CheckFailure(r.status, failureErr)

	default:
		// Unknown status - log and continue checking
		log.Info("RDS instance in unknown status, continuing to monitor")
		return controller.CheckInProgress(r.status)
	}
}

// createInstance handles RDS instance creation using pre-parsed configuration
func (r *RdsOperations) createInstance(ctx context.Context) (*controller.ActionResult, error) {
	config := r.config
	instanceID := config.InstanceID

	log := logf.FromContext(ctx).WithValues("instanceId", instanceID)

	log.Info("Creating RDS instance",
		"instanceID", instanceID,
		"databaseName", config.DatabaseName,
		"instanceClass", config.InstanceClass,
		"databaseEngine", config.DatabaseEngine,
		"region", config.Region)

	// Create RDS instance
	createInput := &rds.CreateDBInstanceInput{
		// Required fields - always convert to pointers
		DBInstanceIdentifier: stringPtr(instanceID),
		DBInstanceClass:      stringPtr(config.InstanceClass),
		Engine:               stringPtr(config.DatabaseEngine),
		EngineVersion:        stringPtr(config.EngineVersion),
		AllocatedStorage:     int32Ptr(config.AllocatedStorage),
		MasterUsername:       stringPtr(config.MasterUsername),
		DBName:               stringPtr(config.DatabaseName),

		// Managed password configuration
		ManageMasterUserPassword: passthroughBoolPtr(config.ManageMasterUserPassword),

		// Optional storage configuration
		StorageType:      optionalStringPtr(config.StorageType),
		StorageEncrypted: passthroughBoolPtr(config.StorageEncrypted),
		KmsKeyId:         optionalStringPtr(config.KmsKeyId),

		// Optional networking configuration
		VpcSecurityGroupIds: config.VpcSecurityGroupIds, // Already []string type
		DBSubnetGroupName:   optionalStringPtr(config.SubnetGroupName),
		PubliclyAccessible:  passthroughBoolPtr(config.PubliclyAccessible),
		Port:                passthroughInt32Ptr(config.Port),

		// Optional backup configuration
		BackupRetentionPeriod: passthroughInt32Ptr(config.BackupRetentionPeriod),
		PreferredBackupWindow: optionalStringPtr(config.PreferredBackupWindow),

		// Optional maintenance configuration
		PreferredMaintenanceWindow: optionalStringPtr(config.PreferredMaintenanceWindow),
		AutoMinorVersionUpgrade:    passthroughBoolPtr(config.AutoMinorVersionUpgrade),

		// Optional performance configuration
		MultiAZ:                   passthroughBoolPtr(config.MultiAZ),
		EnablePerformanceInsights: passthroughBoolPtr(config.PerformanceInsightsEnabled),
		MonitoringInterval:        passthroughPositiveInt32Ptr(config.MonitoringInterval),

		// Deletion protection
		DeletionProtection: passthroughBoolPtr(config.DeletionProtection),
	}

	// AWS doesn't ignore a nil KMS ID for this arg, so we must set it only if provided
	if config.MasterUserSecretKmsKeyId != "" {
		createInput.MasterUserSecretKmsKeyId = stringPtr(config.MasterUserSecretKmsKeyId)
	}

	result, err := r.rdsClient.CreateDBInstance(ctx, createInput)
	if err != nil {
		createErr := fmt.Errorf("failed to create RDS instance: %w", err)
		log.Error(createErr, "Failed to create RDS instance")
		return controller.ActionResultForError(r.status, createErr, rdsErrorClassifier)
	}

	// Update status with deployment information
	r.updateStatus(result.DBInstance)

	// Capture managed password secret ARN from RDS response
	// AWS RDS guarantees this is present when ManageMasterUserPassword=true
	r.status.MasterUserSecretArn = *result.DBInstance.MasterUserSecret.SecretArn
	log.Info("Captured RDS managed password secret ARN", "secretArn", r.status.MasterUserSecretArn)

	log.Info("RDS instance creation initiated successfully", "status", r.status.InstanceStatus)

	return controller.ActionSuccess(r.status)
}

// modifyInstance handles RDS instance modification using pre-parsed configuration
func (r *RdsOperations) modifyInstance(ctx context.Context) (*controller.ActionResult, error) {
	config := r.config
	instanceID := r.config.InstanceID

	log := logf.FromContext(ctx).WithValues("instanceId", instanceID)

	log.Info("Modifying RDS instance")

	// Build modify input with all config values - AWS RDS handles idempotency
	input := &rds.ModifyDBInstanceInput{
		DBInstanceIdentifier:       stringPtr(instanceID),
		DBInstanceClass:            stringPtr(config.InstanceClass),
		AllocatedStorage:           int32Ptr(config.AllocatedStorage),
		EngineVersion:              stringPtr(config.EngineVersion),
		BackupRetentionPeriod:      passthroughInt32Ptr(config.BackupRetentionPeriod),
		MultiAZ:                    passthroughBoolPtr(config.MultiAZ),
		PreferredBackupWindow:      optionalStringPtr(config.PreferredBackupWindow),
		PreferredMaintenanceWindow: optionalStringPtr(config.PreferredMaintenanceWindow),
		AutoMinorVersionUpgrade:    passthroughBoolPtr(config.AutoMinorVersionUpgrade),
		DeletionProtection:         passthroughBoolPtr(config.DeletionProtection),
		// TODO: Figure out how to make this configurable.
		// Right now we need this to be immediate because users need to take down deletion protection fast prior to cleanup
		ApplyImmediately: boolPtr(true),
	}

	result, err := r.rdsClient.ModifyDBInstance(ctx, input)
	if err != nil {
		modifyErr := fmt.Errorf("failed to modify RDS instance: %w", err)
		log.Error(modifyErr, "Failed to modify RDS instance")
		return controller.ActionResultForError(r.status, modifyErr, rdsErrorClassifier)
	}

	// Update status with modification information
	r.updateStatus(result.DBInstance)

	log.Info("RDS instance modification initiated successfully", "status", r.status.InstanceStatus)

	return controller.ActionSuccess(r.status)
}
