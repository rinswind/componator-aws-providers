// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package rds

import (
	"context"
	"fmt"

	"github.com/rinswind/componator/componentkit/functional"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// applyAction handles all RDS-specific deployment operations
func applyAction(
	ctx context.Context,
	name types.NamespacedName,
	spec RdsConfig,
	status RdsStatus) (*functional.ActionResult[RdsStatus], error) {

	// Validate and apply defaults to config
	if err := resolveSpec(&spec); err != nil {
		return functional.ActionFailure(status, fmt.Sprintf("config validation failed: %v", err))
	}

	instanceID := spec.InstanceID

	log := logf.FromContext(ctx).WithValues("instanceId", instanceID)
	log.Info("Starting RDS deployment")

	// Check if the instance exists
	instance, err := getInstanceData(ctx, instanceID)
	if err != nil {
		return functional.ActionResultForError(status, fmt.Errorf("failed to check RDS instance existence: %w", err), rdsErrorClassifier)
	}

	if instance != nil {
		log.Info("RDS instance exists, modifying existing instance")
		instance, err = modifyInstance(ctx, &spec)
		if err != nil {
			return functional.ActionResultForError(status, err, rdsErrorClassifier)
		}

		// Update status with modification information
		updateStatusFromInstance(&status, instance)

		details := fmt.Sprintf("Modifying RDS instance %s (%s)", instanceID, spec.InstanceClass)
		return functional.ActionSuccess(status, details)
	}

	log.Info("RDS instance does not exist, creating new instance")
	instance, err = createInstance(ctx, &spec)
	if err != nil {
		return functional.ActionResultForError(status, err, rdsErrorClassifier)
	}

	// Update status with deployment information
	updateStatusFromInstance(&status, instance)

	// Capture managed password secret ARN from RDS response
	// AWS RDS guarantees this is present when ManageMasterUserPassword=true
	if instance.MasterUserSecret != nil && instance.MasterUserSecret.SecretArn != nil {
		status.MasterUserSecretArn = *instance.MasterUserSecret.SecretArn
		log.Info("Captured RDS managed password secret ARN", "secretArn", status.MasterUserSecretArn)
	}

	details := fmt.Sprintf("Creating RDS instance %s (%s)", instanceID, spec.InstanceClass)
	return functional.ActionSuccess(status, details)
}

// checkApplied verifies the current deployment status
func checkApplied(
	ctx context.Context,
	name types.NamespacedName,
	spec RdsConfig,
	status RdsStatus) (*functional.CheckResult[RdsStatus], error) {

	instanceID := spec.InstanceID

	log := logf.FromContext(ctx).WithValues("instanceId", instanceID)
	log.Info("Checking RDS deployment status")

	// Query RDS instance status
	instance, err := getInstanceData(ctx, instanceID)
	if err != nil {
		return functional.CheckResultForError(status, fmt.Errorf("failed to describe RDS instance: %w", err), rdsErrorClassifier)
	}

	if instance == nil {
		return functional.CheckResultForError(status,
			fmt.Errorf("RDS instance %s not found during deployment check", instanceID), rdsErrorClassifier)
	}

	// Update status with current instance information
	updateStatusFromInstance(&status, instance)

	// Check if deployment is complete
	log = log.WithValues("status", status.InstanceStatus)

	switch status.InstanceStatus {
	case "available":
		log.Info("RDS instance deployment completed successfully",
			"endpoint", status.Endpoint,
			"port", status.Port)

		details := fmt.Sprintf("Instance %s available at %s:%d", instanceID, status.Endpoint, status.Port)
		return functional.CheckComplete(status, details)

	case "creating", "backing-up", "modifying":
		// Still in progress
		log.Info("RDS instance deployment in progress")
		details := fmt.Sprintf("Instance %s status: %s", instanceID, status.InstanceStatus)
		return functional.CheckInProgress(status, details)

	case "failed", "incompatible-restore", "incompatible-network":
		// Failed states
		return functional.CheckResultForError(status,
			fmt.Errorf("RDS instance deployment failed with status: %s", status.InstanceStatus), rdsErrorClassifier)

	default:
		// Unknown status - continue checking
		log.Info("RDS instance in unknown status, continuing to monitor")
		return functional.CheckInProgress(status, "")
	}
}

// deleteAction handles all RDS-specific deletion operations
func deleteAction(
	ctx context.Context,
	name types.NamespacedName,
	spec RdsConfig,
	status RdsStatus) (*functional.ActionResult[RdsStatus], error) {

	instanceID := spec.InstanceID

	log := logf.FromContext(ctx).WithValues("instanceId", instanceID)

	instance, err := deleteInstance(ctx, &spec)
	if err != nil {
		return functional.ActionResultForError(status, err, rdsErrorClassifier)
	}

	if instance == nil {
		// Already deleted
		log.Info("RDS instance already deleted")
		return functional.ActionSuccess(status, "RDS instance already deleted")
	}

	// Update status with AWS response data
	updateStatusFromInstance(&status, instance)

	details := fmt.Sprintf("Deleting RDS instance %s", instanceID)
	return functional.ActionSuccess(status, details)
}

// checkDeleted verifies the current deletion status
func checkDeleted(
	ctx context.Context,
	name types.NamespacedName,
	spec RdsConfig,
	status RdsStatus) (*functional.CheckResult[RdsStatus], error) {

	instanceID := spec.InstanceID

	log := logf.FromContext(ctx).WithValues("instanceId", instanceID)
	log.Info("Checking RDS deleted")

	// Query RDS instance existence
	instance, err := getInstanceData(ctx, instanceID)
	if err != nil {
		return functional.CheckResultForError(status,
			fmt.Errorf("failed to describe RDS instance during deletion check: %w", err), rdsErrorClassifier)
	}

	if instance == nil {
		log.Info("RDS instance successfully deleted")
		details := fmt.Sprintf("Instance %s deleted", instanceID)
		return functional.CheckComplete(status, details)
	}

	instanceStatus := stringValue(instance.DBInstanceStatus)

	log = log.WithValues("status", instanceStatus)

	// Update status with current instance information
	status.InstanceStatus = instanceStatus

	log.Info("RDS instance still exists, checking deletion status")

	switch instanceStatus {
	case "deleting":
		// Still deleting - continue waiting
		log.Info("RDS instance deletion in progress")
		details := fmt.Sprintf("Waiting for instance %s deletion", instanceID)
		return functional.CheckInProgress(status, details)

	case "failed":
		// Deletion failed, but don't block cleanup
		log.Info("RDS instance deletion failed, but allowing cleanup to continue")
		return functional.CheckComplete(status, "")

	default:
		// Instance still exists in some other state
		log.Info("RDS instance in unexpected state during deletion, continuing to wait", "status", instanceStatus)
		return functional.CheckInProgress(status, "")
	}
}
