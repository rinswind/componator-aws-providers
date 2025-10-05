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

// Delete handles all RDS-specific deletion operations using pre-parsed configuration
// Implements ComponentOperations.Delete interface method.
func (r *RdsOperations) Delete(ctx context.Context) (*controller.OperationResult, error) {
	config := r.config
	instanceID := r.config.InstanceID

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

	result, err := r.rdsClient.DeleteDBInstance(ctx, deleteInput)
	if err != nil {
		if isInstanceNotFoundError(err) {
			log.Info("RDS instance already deleted")
			return r.successResult()
		}

		// Check if instance is already being deleted
		if isInstanceAlreadyBeingDeletedError(err) {
			log.Info("RDS instance is already being deleted, proceeding to monitor deletion")
			return r.successResult()
		}

		return r.errorResult(ctx, "delete RDS instance call failed", err)
	}

	log.Info("RDS instance deletion initiated successfully")

	// Update status with AWS response data
	r.updateStatus(result.DBInstance)

	return r.successResult() // Don't block on deletion errors - best effort cleanup
}

// CheckDeletion verifies the current deletion status using pre-parsed configuration
// Implements ComponentOperations.CheckDeletion interface method.
func (r *RdsOperations) CheckDeletion(ctx context.Context) (*controller.OperationResult, error) {
	instanceID := r.config.InstanceID

	log := logf.FromContext(ctx).WithValues("instanceId", instanceID)

	log.Info("Checking RDS deleted")

	// Query RDS instance existence
	instance, err := r.getInstanceData(ctx, instanceID)
	if err != nil {
		return r.errorResult(ctx, "failed to describe RDS instance during deletion check", err)
	}

	if instance == nil {
		log.Info("RDS instance successfully deleted")
		return r.successResult()
	}
	status := stringValue(instance.DBInstanceStatus)

	log = log.WithValues("status", status)

	// Update status with current instance information
	r.status.InstanceStatus = status

	log.Info("RDS instance still exists, checking deletion status")

	switch status {
	case "deleting":
		// Still deleting - continue waiting
		log.Info("RDS instance deletion in progress")
		return r.pendingResult()

	case "failed":
		// Deletion failed, but don't block cleanup
		log.Error(
			fmt.Errorf("RDS instance deletion failed"),
			"RDS instance deletion failed, but allowing cleanup to continue")
		return r.successResult()

	default:
		// Instance still exists in some other state
		log.Info("RDS instance in unexpected state during deletion, continuing to wait", "status", status)
		return r.pendingResult()
	}
}
