// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package rds

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/rinswind/componator/componentkit/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Delete handles all RDS-specific deletion operations using pre-parsed configuration
// Implements ComponentOperations.Delete interface method.
func (r *RdsOperations) Delete(ctx context.Context) (*controller.ActionResult, error) {
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
			return controller.ActionSuccess(r.status)
		}

		// Check if instance is already being deleted
		if isInstanceAlreadyBeingDeletedError(err) {
			log.Info("RDS instance is already being deleted, proceeding to monitor deletion")
			return controller.ActionSuccess(r.status)
		}

		deleteErr := fmt.Errorf("delete RDS instance call failed: %w", err)
		log.Error(deleteErr, "Failed to delete RDS instance")
		return controller.ActionResultForError(r.status, deleteErr, rdsErrorClassifier)
	}

	log.Info("RDS instance deletion initiated successfully")

	// Update status with AWS response data
	r.updateStatus(result.DBInstance)

	details := fmt.Sprintf("Deleting RDS instance %s", instanceID)
	return controller.ActionSuccessWithDetails(r.status, details)
}

// CheckDeleted verifies the current deletion status using pre-parsed configuration
// Implements ComponentOperations.CheckDeleted interface method.
func (r *RdsOperations) CheckDeleted(ctx context.Context) (*controller.CheckResult, error) {
	instanceID := r.config.InstanceID

	log := logf.FromContext(ctx).WithValues("instanceId", instanceID)

	log.Info("Checking RDS deleted")

	// Query RDS instance existence
	instance, err := r.getInstanceData(ctx, instanceID)
	if err != nil {
		checkErr := fmt.Errorf("failed to describe RDS instance during deletion check: %w", err)
		log.Error(checkErr, "Failed to check RDS instance deletion status")
		return controller.CheckResultForError(r.status, checkErr, rdsErrorClassifier)
	}

	if instance == nil {
		log.Info("RDS instance successfully deleted")
		details := fmt.Sprintf("Instance %s deleted", instanceID)
		return controller.CheckCompleteWithDetails(r.status, details)
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
		details := fmt.Sprintf("Waiting for instance %s deletion", instanceID)
		return controller.CheckInProgressWithDetails(r.status, details)

	case "failed":
		// Deletion failed, but don't block cleanup
		log.Error(
			fmt.Errorf("RDS instance deletion failed"),
			"RDS instance deletion failed, but allowing cleanup to continue")
		return controller.CheckComplete(r.status)

	default:
		// Instance still exists in some other state
		log.Info("RDS instance in unexpected state during deletion, continuing to wait", "status", status)
		return controller.CheckInProgress(r.status)
	}
}
