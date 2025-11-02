// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package rds

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/rinswind/componator/componentkit/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Package-level singletons initialized during registration
var (
	rdsClient *rds.Client
)

// getInstanceData retrieves RDS instance data, handling not-found cases consistently
func getInstanceData(ctx context.Context, instanceID string) (*types.DBInstance, error) {
	input := &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: stringPtr(instanceID),
	}

	result, err := rdsClient.DescribeDBInstances(ctx, input)
	if err != nil {
		if isInstanceNotFoundError(err) {
			return nil, nil // Instance not found - return nil without error
		}
		return nil, fmt.Errorf("failed to describe RDS instance: %w", err)
	}

	if len(result.DBInstances) == 0 {
		return nil, nil // No instances returned - treat as not found
	}

	return &result.DBInstances[0], nil
}

// createInstance creates an RDS instance
func createInstance(ctx context.Context, config *RdsConfig) (*types.DBInstance, error) {
	instanceID := config.InstanceID

	log := logf.FromContext(ctx).WithValues("instanceId", instanceID)

	log.Info("Creating RDS instance",
		"instanceID", instanceID,
		"databaseName", config.DatabaseName,
		"instanceClass", config.InstanceClass,
		"databaseEngine", config.DatabaseEngine)

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

	result, err := rdsClient.CreateDBInstance(ctx, createInput)
	if err != nil {
		return nil, fmt.Errorf("failed to create RDS instance: %w", err)
	}

	log.Info("RDS instance creation initiated successfully")

	return result.DBInstance, nil
}

// modifyInstance modifies an existing RDS instance
func modifyInstance(ctx context.Context, config *RdsConfig) (*types.DBInstance, error) {
	instanceID := config.InstanceID

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

	result, err := rdsClient.ModifyDBInstance(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to modify RDS instance: %w", err)
	}

	log.Info("RDS instance modification initiated successfully")

	return result.DBInstance, nil
}

// deleteInstance deletes an RDS instance
func deleteInstance(ctx context.Context, config *RdsConfig) (*types.DBInstance, error) {
	instanceID := config.InstanceID

	log := logf.FromContext(ctx).WithValues("instanceId", instanceID)

	// Build delete input
	deleteInput := &rds.DeleteDBInstanceInput{
		DBInstanceIdentifier: stringPtr(instanceID),
	}

	if boolValue(config.SkipFinalSnapshot) {
		deleteInput.SkipFinalSnapshot = boolPtr(true)
	} else {
		deleteInput.SkipFinalSnapshot = boolPtr(false)
		deleteInput.FinalDBSnapshotIdentifier = stringPtr(config.FinalDBSnapshotIdentifier)
	}

	log.Info("Deleting RDS instance", "skipFinalSnapshot", boolValue(deleteInput.SkipFinalSnapshot))

	result, err := rdsClient.DeleteDBInstance(ctx, deleteInput)
	if err != nil {
		if isInstanceNotFoundError(err) {
			log.Info("RDS instance already deleted")
			return nil, nil
		}

		// Check if instance is already being deleted
		if isInstanceAlreadyBeingDeletedError(err) {
			log.Info("RDS instance is already being deleted")
			return nil, nil
		}

		return nil, fmt.Errorf("delete RDS instance call failed: %w", err)
	}

	log.Info("RDS instance deletion initiated successfully")

	return result.DBInstance, nil
}

// updateStatusFromInstance updates RdsStatus fields from AWS DBInstance data
func updateStatusFromInstance(status *RdsStatus, instance *types.DBInstance) {
	if instance == nil {
		return
	}

	status.InstanceStatus = stringValue(instance.DBInstanceStatus)
	status.InstanceARN = stringValue(instance.DBInstanceArn)
	status.Endpoint = endpointAddress(instance.Endpoint)
	status.Port = endpointPort(instance.Endpoint)
	status.AvailabilityZone = stringValue(instance.AvailabilityZone)

	// Preserve or update managed password secret ARN
	// The ARN is immutable once created, but may be present in DescribeDBInstances response
	if instance.MasterUserSecret != nil && instance.MasterUserSecret.SecretArn != nil {
		status.MasterUserSecretArn = *instance.MasterUserSecret.SecretArn
	}
	// If not present in response but already in status, keep existing value (ARN doesn't change)
}

// isInstanceNotFoundError checks if the error indicates the RDS instance was not found
func isInstanceNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific AWS RDS error type
	var notFoundErr *types.DBInstanceNotFoundFault
	return errors.As(err, &notFoundErr)
}

// isInstanceAlreadyBeingDeletedError checks if the error indicates the RDS instance is already being deleted
func isInstanceAlreadyBeingDeletedError(err error) bool {
	if err == nil {
		return false
	}

	// Check for InvalidDBInstanceState error when instance is already being deleted
	var invalidStateErr *types.InvalidDBInstanceStateFault
	if errors.As(err, &invalidStateErr) {
		// Check if the error message indicates the instance is already being deleted
		return strings.Contains(strings.ToLower(invalidStateErr.ErrorMessage()), "already being deleted")
	}

	return false
}

// rdsErrorClassifier wraps the AWS SDK retry logic for use with result builder utilities.
var rdsErrorClassifier = controller.ErrorClassifier(isRetryable)

// isRetryable determines if an error is retryable using AWS SDK's built-in error classification.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Use AWS SDK's built-in retry classification
	// This handles all AWS API errors, network errors, and HTTP status codes
	for _, checker := range retry.DefaultRetryables {
		if checker.IsErrorRetryable(err) == aws.TrueTernary {
			return true
		}
	}

	return false
}
