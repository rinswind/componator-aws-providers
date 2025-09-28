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

	"github.com/aws/aws-sdk-go-v2/aws"
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
	createInput := r.buildCreateDBInstanceInput(config, instanceID)

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
	r.status.CreatedAt = time.Now().Format(time.RFC3339)
	r.status.InstanceStatus = "creating"
	r.status.EngineVersion = config.EngineVersion
	r.status.InstanceClass = config.InstanceClass
	r.status.AllocatedStorage = config.AllocatedStorage

	if result.DBInstance != nil {
		if result.DBInstance.DBInstanceStatus != nil {
			r.status.InstanceStatus = *result.DBInstance.DBInstanceStatus
		}
		if result.DBInstance.Endpoint != nil && result.DBInstance.Endpoint.Address != nil {
			r.status.Endpoint = *result.DBInstance.Endpoint.Address
		}
		if result.DBInstance.Endpoint != nil && result.DBInstance.Endpoint.Port != nil {
			r.status.Port = *result.DBInstance.Endpoint.Port
		}
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
	if instanceID == "" {
		instanceID = fmt.Sprintf("%s-db", config.DatabaseName)
	}

	log.Info("Checking RDS deployment status using pre-parsed configuration",
		"databaseName", config.DatabaseName,
		"instanceId", instanceID,
		"elapsed", elapsed)

	// Check timeout
	createTimeout := 40 * time.Minute // Default timeout
	if config.Timeouts != nil && config.Timeouts.Create != nil {
		createTimeout = config.Timeouts.Create.Duration
	}

	if elapsed > createTimeout {
		timeoutErr := fmt.Errorf("RDS instance creation timed out after %v", elapsed)
		return r.errorResult(ctx, "deployment timeout exceeded", timeoutErr)
	}

	// Query RDS instance status
	input := &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: aws.String(instanceID),
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
	if instance.DBInstanceStatus != nil {
		r.status.InstanceStatus = *instance.DBInstanceStatus
	}
	if instance.Engine != nil {
		r.status.EngineVersion = aws.ToString(instance.EngineVersion)
	}
	if instance.DBInstanceClass != nil {
		r.status.InstanceClass = *instance.DBInstanceClass
	}
	if instance.AllocatedStorage != nil {
		r.status.AllocatedStorage = *instance.AllocatedStorage
	}
	if instance.Endpoint != nil {
		if instance.Endpoint.Address != nil {
			r.status.Endpoint = *instance.Endpoint.Address
		}
		if instance.Endpoint.Port != nil {
			r.status.Port = *instance.Endpoint.Port
		}
	}
	if instance.BackupRetentionPeriod != nil {
		r.status.BackupRetentionPeriod = *instance.BackupRetentionPeriod
	}
	if instance.MultiAZ != nil {
		r.status.MultiAZ = *instance.MultiAZ
	}

	// Update last modified time
	r.status.LastModifiedTime = time.Now().Format(time.RFC3339)

	// Check if deployment is complete
	status := aws.ToString(instance.DBInstanceStatus)
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
