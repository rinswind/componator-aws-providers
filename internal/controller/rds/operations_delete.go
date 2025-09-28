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

// Delete handles all RDS-specific deletion operations using pre-parsed configuration
// Implements ComponentOperations.Delete interface method.
func (r *RdsOperations) Delete(ctx context.Context) (*base.OperationResult, error) {
	log := logf.FromContext(ctx)

	// Use pre-parsed configuration from factory (no repeated parsing)
	config := r.config

	instanceID := r.status.InstanceID
	if instanceID == "" {
		instanceID = fmt.Sprintf("%s-db", config.DatabaseName)
	}

	log.Info("Starting RDS deletion using pre-parsed configuration",
		"databaseName", config.DatabaseName,
		"instanceId", instanceID)

	// Check if instance exists
	exists, err := r.checkInstanceExists(ctx, instanceID)
	if err != nil {
		// Log error but don't block deletion - instance might already be gone
		log.Error(err, "Failed to check instance existence during deletion, continuing anyway")
	}

	if !exists {
		log.Info("RDS instance already deleted or doesn't exist",
			"instanceId", instanceID)

		return r.successResult()
	}

	// Build delete input
	deleteInput := &rds.DeleteDBInstanceInput{
		DBInstanceIdentifier: stringPtr(instanceID),
	}

	// Configure final snapshot behavior
	if config.SkipFinalSnapshot != nil && *config.SkipFinalSnapshot {
		deleteInput.SkipFinalSnapshot = boolPtr(true)
	} else {
		deleteInput.SkipFinalSnapshot = boolPtr(false)

		// Generate final snapshot identifier if not provided
		finalSnapshotID := config.FinalDBSnapshotIdentifier
		if finalSnapshotID == "" {
			finalSnapshotID = fmt.Sprintf("%s-final-snapshot-%d", instanceID, time.Now().Unix())
		}
		deleteInput.FinalDBSnapshotIdentifier = stringPtr(finalSnapshotID)
	}

	// Delete protection must be disabled before deletion
	if config.DeletionProtection != nil && *config.DeletionProtection {
		log.Info("Disabling deletion protection before deleting RDS instance",
			"instanceId", instanceID)

		modifyInput := &rds.ModifyDBInstanceInput{
			DBInstanceIdentifier: stringPtr(instanceID),
			DeletionProtection:   boolPtr(false),
			ApplyImmediately:     boolPtr(true),
		}

		_, err := r.rdsClient.ModifyDBInstance(ctx, modifyInput)
		if err != nil {
			log.Error(err, "Failed to disable deletion protection, attempting deletion anyway")
		} else {
			log.Info("Deletion protection disabled successfully")
		}
	}

	// Initiate RDS instance deletion
	log.Info("Deleting RDS instance",
		"instanceId", instanceID,
		"skipFinalSnapshot", boolValue(deleteInput.SkipFinalSnapshot))

	_, err = r.rdsClient.DeleteDBInstance(ctx, deleteInput)
	if err != nil {
		if r.isInstanceNotFoundError(err) {
			log.Info("RDS instance already deleted",
				"instanceId", instanceID)
		} else {
			log.Error(err, "Failed to delete RDS instance, continuing anyway to avoid blocking cleanup")
		}
	} else {
		log.Info("RDS instance deletion initiated successfully",
			"instanceId", instanceID)
	}

	// Update status to track deletion initiation
	r.status.LastModifiedTime = time.Now().Format(time.RFC3339)
	r.status.InstanceStatus = "deleting"

	return r.successResult() // Don't block on deletion errors - best effort cleanup
}

// CheckDeletion verifies the current deletion status using pre-parsed configuration
// Implements ComponentOperations.CheckDeletion interface method.
func (r *RdsOperations) CheckDeletion(ctx context.Context, elapsed time.Duration) (*base.OperationResult, error) {
	log := logf.FromContext(ctx)

	// Use pre-parsed configuration from factory (no repeated parsing)
	config := r.config

	instanceID := r.status.InstanceID
	if instanceID == "" {
		instanceID = fmt.Sprintf("%s-db", config.DatabaseName)
	}

	log.Info("Checking RDS deletion status using pre-parsed configuration",
		"databaseName", config.DatabaseName,
		"instanceId", instanceID,
		"elapsed", elapsed)

	// Check timeout
	deleteTimeout := 60 * time.Minute // Default timeout
	if config.Timeouts != nil && config.Timeouts.Delete != nil {
		deleteTimeout = config.Timeouts.Delete.Duration
	}

	if elapsed > deleteTimeout {
		log.Info("RDS deletion timeout exceeded, assuming deletion completed",
			"instanceId", instanceID,
			"elapsed", elapsed,
			"timeout", deleteTimeout)

		return r.successResult() // Don't block cleanup on timeout
	}

	// Query RDS instance existence
	input := &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: stringPtr(instanceID),
	}

	result, err := r.rdsClient.DescribeDBInstances(ctx, input)
	if err != nil {
		if r.isInstanceNotFoundError(err) {
			log.Info("RDS instance successfully deleted",
				"instanceId", instanceID,
				"elapsed", elapsed)

			return r.successResult()
		}

		// For transient errors, continue checking
		if r.isTransientError(err) {
			log.Info("Transient error checking RDS deletion status, will retry",
				"instanceId", instanceID,
				"error", err.Error())

			return r.pendingResult() // Return error to trigger retry
		}

		// For other errors, log but don't block deletion
		log.Error(err, "Error checking RDS deletion status, assuming deletion completed")
		return r.successResult() // Don't block cleanup on API errors
	}

	if len(result.DBInstances) == 0 {
		log.Info("RDS instance successfully deleted (no instances returned)",
			"instanceId", instanceID,
			"elapsed", elapsed)

		return r.successResult()
	}

	instance := result.DBInstances[0]
	status := stringValue(instance.DBInstanceStatus)

	// Update status with current instance information
	r.status.InstanceStatus = status
	r.status.LastModifiedTime = time.Now().Format(time.RFC3339)

	log.Info("RDS instance still exists, checking deletion status",
		"instanceId", instanceID,
		"status", status,
		"elapsed", elapsed)

	switch status {
	case "deleting":
		// Still deleting - continue waiting
		log.Info("RDS instance deletion in progress",
			"instanceId", instanceID,
			"elapsed", elapsed)

		return r.pendingResult()

	case "failed":
		// Deletion failed, but don't block cleanup
		log.Error(fmt.Errorf("RDS instance deletion failed"),
			"RDS instance deletion failed, but allowing cleanup to continue",
			"instanceId", instanceID)

		return r.successResult()

	default:
		// Instance still exists in some other state
		// Log but continue waiting - instance might still be deleting
		log.Info("RDS instance in unexpected state during deletion, continuing to wait",
			"instanceId", instanceID,
			"status", status,
			"elapsed", elapsed)

		return r.pendingResult()
	}
}
