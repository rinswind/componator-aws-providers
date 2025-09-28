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

	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/rinswind/deployment-operator/handler/base"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Upgrade handles RDS-specific upgrade operations using pre-parsed configuration
// Implements ComponentOperations.Upgrade interface method.
func (r *RdsOperations) Upgrade(ctx context.Context) (*base.OperationResult, error) {
	config := r.config

	instanceID := r.config.InstanceID

	log := logf.FromContext(ctx).WithValues("instanceId", instanceID)

	log.Info("Starting RDS upgrade using pre-parsed configuration", "databaseName", config.DatabaseName)

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

	log.Info("Applying RDS modifications")

	result, err := r.rdsClient.ModifyDBInstance(ctx, input)
	if err != nil {
		return r.errorResult(ctx, "failed to modify RDS instance", err)
	}

	// Update status with modification information
	r.status.InstanceStatus = stringValue(result.DBInstance.DBInstanceStatus)
	r.status.InstanceClass = stringValue(result.DBInstance.DBInstanceClass)
	r.status.AllocatedStorage = int32Value(result.DBInstance.AllocatedStorage)

	log.Info("RDS instance modification initiated successfully", "status", r.status.InstanceStatus)

	return r.pendingResult() // Still modifying, need to check status
}
