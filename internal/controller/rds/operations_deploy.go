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
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/rinswind/deployment-operator/handler/base"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Deploy handles all RDS-specific deployment operations using pre-parsed configuration
// Implements ComponentOperations.Deploy interface method.
func (r *RdsOperations) Deploy(ctx context.Context) (*base.OperationResult, error) {
	log := logf.FromContext(ctx)

	// Use pre-parsed configuration from factory (no repeated parsing)
	config := r.config

	log.Info("Starting RDS deployment using pre-parsed configuration",
		"databaseName", config.DatabaseName,
		"instanceClass", config.InstanceClass,
		"databaseEngine", config.DatabaseEngine,
		"region", config.Region)

	// Generate instance identifier if not already set
	instanceID := fmt.Sprintf("%s-db", config.DatabaseName)
	if r.status.InstanceID != "" {
		instanceID = r.status.InstanceID
	}

	// Check if instance already exists
	existing, err := r.checkInstanceExists(ctx, instanceID)
	if err != nil {
		return r.errorResult(ctx, "failed to check if RDS instance exists", err)
	}

	if existing {
		log.Info("RDS instance already exists, checking status", "instanceId", instanceID)
		return r.CheckDeployment(ctx, 0)
	}

	// Create RDS instance
	createInput := buildCreateDBInstanceInput(config, instanceID)

	log.Info("Creating RDS instance",
		"instanceId", instanceID,
		"databaseEngine", config.DatabaseEngine,
		"instanceClass", config.InstanceClass)

	result, err := r.rdsClient.CreateDBInstance(ctx, createInput)
	if err != nil {
		return r.errorResult(ctx, "failed to create RDS instance", err)
	}

	// Update status with deployment information
	r.status.DatabaseName = config.DatabaseName
	r.status.InstanceID = instanceID
	r.status.InstanceStatus = "creating"
	r.status.EngineVersion = config.EngineVersion
	r.status.InstanceClass = config.InstanceClass
	r.status.AllocatedStorage = config.AllocatedStorage

	if result.DBInstance != nil {
		r.status.InstanceStatus = stringValue(result.DBInstance.DBInstanceStatus)
		r.status.Endpoint = endpointAddress(result.DBInstance.Endpoint)
		r.status.Port = endpointPort(result.DBInstance.Endpoint)
	}

	log.Info("RDS instance creation initiated successfully",
		"instanceId", instanceID,
		"status", r.status.InstanceStatus)

	return r.pendingResult() // Still creating, need to check status
}

// CheckDeployment verifies the current deployment status using pre-parsed configuration
// Implements ComponentOperations.CheckDeployment interface method.
func (r *RdsOperations) CheckDeployment(ctx context.Context, elapsed time.Duration) (*base.OperationResult, error) {
	log := logf.FromContext(ctx)

	// Use pre-parsed configuration from factory (no repeated parsing)
	config := r.config

	instanceID := r.status.InstanceID

	log.Info("Checking RDS deployment status using pre-parsed configuration",
		"databaseName", config.DatabaseName,
		"instanceId", instanceID,
		"elapsed", elapsed)

	// Check timeout
	if elapsed > config.Timeouts.Create.Duration {
		timeoutErr := fmt.Errorf("RDS instance creation timed out after %v", elapsed)
		return r.errorResult(ctx, "deployment timeout exceeded", timeoutErr)
	}

	// Query RDS instance status
	input := &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: stringPtr(instanceID),
	}

	result, err := r.rdsClient.DescribeDBInstances(ctx, input)
	if err != nil {
		if r.isInstanceNotFoundError(err) {
			notFoundErr := fmt.Errorf("RDS instance %s not found during deployment check", instanceID)
			return r.errorResult(ctx, "instance not found", notFoundErr)
		}
		return r.errorResult(ctx, "failed to describe RDS instance", err)
	}

	if len(result.DBInstances) == 0 {
		notFoundErr := fmt.Errorf("no RDS instances returned for identifier %s", instanceID)
		return r.errorResult(ctx, "no instances found", notFoundErr)
	}

	instance := result.DBInstances[0]

	// Update status with current instance information
	r.status.InstanceID = instanceID
	r.status.InstanceStatus = stringValue(instance.DBInstanceStatus)
	r.status.EngineVersion = stringValue(instance.EngineVersion)
	r.status.InstanceClass = stringValue(instance.DBInstanceClass)
	r.status.AllocatedStorage = int32Value(instance.AllocatedStorage)
	r.status.Endpoint = endpointAddress(instance.Endpoint)
	r.status.Port = endpointPort(instance.Endpoint)
	r.status.BackupRetentionPeriod = int32Value(instance.BackupRetentionPeriod)
	r.status.MultiAZ = boolValue(instance.MultiAZ)

	// Check if deployment is complete
	status := stringValue(instance.DBInstanceStatus)
	log.Info("RDS instance status check",
		"instanceId", instanceID,
		"status", status,
		"elapsed", elapsed)

	switch status {
	case "available":
		log.Info("RDS instance deployment completed successfully",
			"instanceId", instanceID,
			"endpoint", r.status.Endpoint,
			"port", r.status.Port)

		return r.successResult()

	case "creating", "backing-up", "modifying":
		// Still in progress
		log.Info("RDS instance deployment in progress",
			"instanceId", instanceID,
			"status", status)

		return r.pendingResult()

	case "failed", "incompatible-restore", "incompatible-network":
		// Failed states
		failureErr := fmt.Errorf("RDS instance deployment failed with status: %s", status)
		return r.errorResult(ctx, "deployment failed", failureErr)

	default:
		// Unknown status - log and continue checking
		log.Info("RDS instance in unknown status, continuing to monitor",
			"instanceId", instanceID,
			"status", status)

		return r.pendingResult()
	}
}

// buildCreateDBInstanceInput constructs the CreateDBInstanceInput from RDS configuration
func buildCreateDBInstanceInput(config *RdsConfig, instanceID string) *rds.CreateDBInstanceInput {
	return &rds.CreateDBInstanceInput{
		// Required fields - always convert to pointers
		DBInstanceIdentifier: stringPtr(instanceID),
		DBInstanceClass:      stringPtr(config.InstanceClass),
		Engine:               stringPtr(config.DatabaseEngine),
		EngineVersion:        stringPtr(config.EngineVersion),
		AllocatedStorage:     int32Ptr(config.AllocatedStorage),
		MasterUsername:       stringPtr(config.MasterUsername),
		MasterUserPassword:   stringPtr(config.MasterPassword),
		DBName:               stringPtr(config.DatabaseName),

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
}
