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
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/rinswind/deployment-operator/handler/base"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Upgrade handles RDS-specific upgrade operations using pre-parsed configuration
// Implements ComponentOperations.Upgrade interface method.
func (r *RdsOperations) Upgrade(ctx context.Context) (*base.OperationResult, error) {
	log := logf.FromContext(ctx)

	// Use pre-parsed configuration from factory (no repeated parsing)
	config := r.config

	instanceID := r.status.InstanceID
	if instanceID == "" {
		instanceID = fmt.Sprintf("%s-db", config.DatabaseName)
	}

	log.Info("Starting RDS upgrade using pre-parsed configuration",
		"databaseName", config.DatabaseName,
		"instanceId", instanceID)

	// Get current instance state
	currentInstance, err := r.getCurrentInstanceState(ctx, instanceID)
	if err != nil {
		if r.isInstanceNotFoundError(err) {
			// If instance doesn't exist, treat as new deployment
			log.Info("RDS instance not found during upgrade, treating as new deployment",
				"instanceId", instanceID)
			return r.Deploy(ctx)
		}
		return r.handleOperationError(ctx, "failed to get current RDS instance state", err)
	}

	// Check if instance is in a modifiable state
	currentStatus := aws.ToString(currentInstance.DBInstanceStatus)
	if !r.isInstanceModifiable(currentStatus) {
		log.Info("RDS instance not in modifiable state, waiting",
			"instanceId", instanceID,
			"status", currentStatus)

		return r.pendingResult(), nil
	}

	// Calculate required modifications
	modifications := r.calculateRequiredModifications(currentInstance, config)
	if len(modifications) == 0 {
		log.Info("No modifications required for RDS instance",
			"instanceId", instanceID)

		return r.successResult(), nil
	}

	// Apply modifications
	modifyInput := r.buildModifyDBInstanceInput(config, instanceID, modifications)

	log.Info("Applying RDS modifications",
		"instanceId", instanceID,
		"modifications", len(modifications))

	result, err := r.rdsClient.ModifyDBInstance(ctx, modifyInput)
	if err != nil {
		return r.handleOperationError(ctx, "failed to modify RDS instance", err)
	}

	// Update status with upgrade information
	r.status.LastModifiedTime = time.Now().Format(time.RFC3339)
	r.status.InstanceStatus = "modifying"

	if result.DBInstance != nil {
		if result.DBInstance.DBInstanceStatus != nil {
			r.status.InstanceStatus = *result.DBInstance.DBInstanceStatus
		}
		// Update other fields as needed
		if result.DBInstance.DBInstanceClass != nil {
			r.status.InstanceClass = *result.DBInstance.DBInstanceClass
		}
		if result.DBInstance.AllocatedStorage != nil {
			r.status.AllocatedStorage = *result.DBInstance.AllocatedStorage
		}
	}

	log.Info("RDS instance modification initiated successfully",
		"instanceId", instanceID,
		"status", r.status.InstanceStatus)

	return r.pendingResult(), nil // Still modifying, need to check status
}

// getCurrentInstanceState retrieves the current state of an RDS instance
func (r *RdsOperations) getCurrentInstanceState(ctx context.Context, instanceID string) (*types.DBInstance, error) {
	input := &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: aws.String(instanceID),
	}

	result, err := r.rdsClient.DescribeDBInstances(ctx, input)
	if err != nil {
		return nil, err
	}

	if len(result.DBInstances) == 0 {
		return nil, fmt.Errorf("no RDS instances found with identifier %s", instanceID)
	}

	return &result.DBInstances[0], nil
}

// isInstanceModifiable checks if the RDS instance is in a state where it can be modified
func (r *RdsOperations) isInstanceModifiable(status string) bool {
	modifiableStates := []string{
		"available",
		"storage-optimization",
	}

	for _, state := range modifiableStates {
		if status == state {
			return true
		}
	}

	return false
}

// ModificationRequest represents a single modification to be applied to an RDS instance
type ModificationRequest struct {
	Type        string
	Description string
	Required    bool
}

// calculateRequiredModifications compares current and desired state to determine required changes
func (r *RdsOperations) calculateRequiredModifications(currentInstance *types.DBInstance, config *RdsConfig) []ModificationRequest {
	var modifications []ModificationRequest

	// Check instance class
	if currentInstance.DBInstanceClass != nil && *currentInstance.DBInstanceClass != config.InstanceClass {
		modifications = append(modifications, ModificationRequest{
			Type:        "instance_class",
			Description: fmt.Sprintf("Change instance class from %s to %s", *currentInstance.DBInstanceClass, config.InstanceClass),
			Required:    true,
		})
	}

	// Check allocated storage (can only increase)
	if currentInstance.AllocatedStorage != nil && *currentInstance.AllocatedStorage < config.AllocatedStorage {
		modifications = append(modifications, ModificationRequest{
			Type:        "allocated_storage",
			Description: fmt.Sprintf("Increase storage from %d to %d GB", *currentInstance.AllocatedStorage, config.AllocatedStorage),
			Required:    true,
		})
	}

	// Check engine version (can only upgrade)
	if currentInstance.EngineVersion != nil && *currentInstance.EngineVersion != config.EngineVersion {
		modifications = append(modifications, ModificationRequest{
			Type:        "engine_version",
			Description: fmt.Sprintf("Upgrade engine version from %s to %s", *currentInstance.EngineVersion, config.EngineVersion),
			Required:    true,
		})
	}

	// Check backup retention period
	if config.BackupRetentionPeriod != nil &&
		(currentInstance.BackupRetentionPeriod == nil || *currentInstance.BackupRetentionPeriod != *config.BackupRetentionPeriod) {
		modifications = append(modifications, ModificationRequest{
			Type:        "backup_retention",
			Description: fmt.Sprintf("Change backup retention period to %d days", *config.BackupRetentionPeriod),
			Required:    false,
		})
	}

	// Check Multi-AZ configuration
	if config.MultiAZ != nil &&
		(currentInstance.MultiAZ == nil || *currentInstance.MultiAZ != *config.MultiAZ) {
		modifications = append(modifications, ModificationRequest{
			Type:        "multi_az",
			Description: fmt.Sprintf("Change Multi-AZ configuration to %t", *config.MultiAZ),
			Required:    false,
		})
	}

	return modifications
}

// buildModifyDBInstanceInput constructs the ModifyDBInstanceInput for applying changes
func (r *RdsOperations) buildModifyDBInstanceInput(config *RdsConfig, instanceID string, modifications []ModificationRequest) *rds.ModifyDBInstanceInput {
	input := &rds.ModifyDBInstanceInput{
		DBInstanceIdentifier: aws.String(instanceID),
		ApplyImmediately:     aws.Bool(false), // Apply during next maintenance window by default
	}

	// Apply modifications based on the calculated requirements
	for _, mod := range modifications {
		switch mod.Type {
		case "instance_class":
			input.DBInstanceClass = aws.String(config.InstanceClass)
		case "allocated_storage":
			input.AllocatedStorage = aws.Int32(config.AllocatedStorage)
		case "engine_version":
			input.EngineVersion = aws.String(config.EngineVersion)
		case "backup_retention":
			if config.BackupRetentionPeriod != nil {
				input.BackupRetentionPeriod = aws.Int32(*config.BackupRetentionPeriod)
			}
		case "multi_az":
			if config.MultiAZ != nil {
				input.MultiAZ = aws.Bool(*config.MultiAZ)
			}
		}
	}

	// Add other optional modifications
	if config.PreferredBackupWindow != "" {
		input.PreferredBackupWindow = aws.String(config.PreferredBackupWindow)
	}
	if config.PreferredMaintenanceWindow != "" {
		input.PreferredMaintenanceWindow = aws.String(config.PreferredMaintenanceWindow)
	}
	if config.AutoMinorVersionUpgrade != nil {
		input.AutoMinorVersionUpgrade = aws.Bool(*config.AutoMinorVersionUpgrade)
	}

	return input
}
