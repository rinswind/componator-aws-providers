// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package rds

import (
	"context"
	"fmt"

	"github.com/rinswind/componator/componentkit/controller"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// checkHealth performs runtime health monitoring of Ready RDS instances.
// This is called periodically while the Component is in Ready state.
//
// Health evaluation focuses on operational status that affects database availability:
//   - Healthy: instance is operational and accepting connections
//   - Degraded: instance has operational issues (storage full, maintenance, stopped)
//
// Health checks do not trigger phase transitions - they only update the Degraded condition.
func checkHealth(
	ctx context.Context,
	name types.NamespacedName,
	spec RdsConfig,
	status RdsStatus) (*controller.HealthCheckResult, error) {

	instanceID := spec.InstanceID
	log := logf.FromContext(ctx).WithValues("instanceId", instanceID)

	// Query current RDS instance status
	instance, err := getInstanceData(ctx, instanceID)
	if err != nil {
		// Classify error to determine if retryable or degraded
		return controller.HealthCheckResultForError(err, rdsErrorClassifier, "APIError")
	}

	// Instance not found - external deletion detected
	if instance == nil {
		log.Info("RDS instance not found during health check - may have been deleted externally")
		return controller.HealthCheckDegraded(
			"InstanceDeleted",
			fmt.Sprintf("RDS instance %s not found in AWS", instanceID))
	}

	instanceStatus := stringValue(instance.DBInstanceStatus)
	log.V(1).Info("Checking RDS instance health", "status", instanceStatus)

	// Evaluate health based on instance status
	switch RDSInstanceStatus(instanceStatus) {
	case StatusAvailable, StatusStorageOptimization, StatusBackingUp,
		StatusConfiguringEnhancedMonitoring, StatusConfiguringIAMDatabaseAuth,
		StatusConfiguringLogExports, StatusModifying:
		// Instance is operational and accepting connections
		// These states don't prevent normal database operations
		// - modifying: most changes don't cause downtime
		return controller.HealthCheckHealthy(
			fmt.Sprintf("Instance %s is operational (status: %s)", instanceID, instanceStatus))

	case StatusStorageFull:
		// Instance storage capacity exhausted - connections may fail
		return controller.HealthCheckDegraded(
			"StorageFull",
			fmt.Sprintf("Instance %s storage capacity exhausted", instanceID))

	case StatusMaintenance, StatusRebooting:
		// Instance undergoing maintenance or restarting - temporarily unavailable
		return controller.HealthCheckDegraded(
			"Maintenance",
			fmt.Sprintf("Instance %s undergoing maintenance (status: %s)", instanceID, instanceStatus))

	case StatusStopped, StatusStopping, StatusStarting:
		// Instance not running or transitioning power state
		return controller.HealthCheckDegraded(
			"Stopped",
			fmt.Sprintf("Instance %s is not running (status: %s)", instanceID, instanceStatus))

	case StatusFailed, StatusInaccessibleEncryptionCredentials,
		StatusIncompatibleNetwork, StatusIncompatibleOptionGroup,
		StatusIncompatibleParameters, StatusIncompatibleRestore,
		StatusInsufficientCapacity:
		// Instance in error state - not operational
		return controller.HealthCheckDegraded(
			"Failed",
			fmt.Sprintf("Instance %s in error state: %s", instanceID, instanceStatus))

	default:
		// Unknown status - treat as degraded to surface the issue
		log.Info("RDS instance in unknown status during health check", "status", instanceStatus)
		return controller.HealthCheckDegraded(
			"UnknownStatus",
			fmt.Sprintf("Instance %s in unknown state: %s", instanceID, instanceStatus))
	}
}
