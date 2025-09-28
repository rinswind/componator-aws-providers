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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/rinswind/deployment-operator/handler/base"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// HandlerName is the identifier for this RDS handler
	HandlerName = "rds"

	ControllerName = "rds-component"
)

// RdsOperationsFactory implements the ComponentOperationsFactory interface for RDS deployments.
// This factory creates stateful RdsOperations instances with pre-parsed configuration,
// eliminating repeated configuration parsing during reconciliation loops.
type RdsOperationsFactory struct{}

// CreateOperations creates a new stateful RdsOperations instance with pre-parsed configuration and status.
// This method is called once per reconciliation loop to eliminate repeated configuration parsing.
//
// The returned RdsOperations instance maintains the parsed configuration and status and can be used
// throughout the reconciliation loop without re-parsing the same configuration multiple times.
func (f *RdsOperationsFactory) CreateOperations(ctx context.Context, config json.RawMessage, currentStatus json.RawMessage) (base.ComponentOperations, error) {
	log := logf.FromContext(ctx)

	// Parse configuration once for this reconciliation loop
	rdsConfig, err := resolveRdsConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse rds configuration: %w", err)
	}

	// Parse existing handler status
	status, err := resolveRdsStatus(ctx, currentStatus)
	if err != nil {
		return nil, fmt.Errorf("failed to parse rds status: %w", err)
	}

	// Initialize AWS configuration for the specified region
	awsConfig, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(rdsConfig.Region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS configuration for region %s: %w", rdsConfig.Region, err)
	}

	// Create RDS client with the configured region
	rdsClient := rds.NewFromConfig(awsConfig)

	log.V(1).Info("Created RDS operations with AWS client",
		"region", rdsConfig.Region,
		"databaseEngine", rdsConfig.DatabaseEngine,
		"instanceClass", rdsConfig.InstanceClass)

	// Return stateful operations instance with pre-parsed configuration and status
	return &RdsOperations{
		config:    rdsConfig,
		status:    status,
		rdsClient: rdsClient,
		awsConfig: awsConfig,
	}, nil
}

// RdsOperations implements the ComponentOperations interface for RDS-based deployments.
// This struct provides all RDS-specific deployment, upgrade, and deletion operations
// for managing AWS RDS instances through the AWS SDK with pre-parsed configuration.
//
// This is a stateful operations instance created by RdsOperationsFactory that eliminates
// repeated configuration parsing by maintaining parsed configuration state.
type RdsOperations struct {
	// config holds the pre-parsed RDS configuration for this reconciliation loop
	config *RdsConfig

	// status holds the pre-parsed RDS status for this reconciliation loop
	status *RdsStatus

	// AWS SDK clients for RDS operations
	rdsClient *rds.Client
	awsConfig aws.Config
}

// NewRdsOperationsFactory creates a new RdsOperationsFactory instance
func NewRdsOperationsFactory() *RdsOperationsFactory {
	return &RdsOperationsFactory{}
}

// NewRdsOperationsConfig creates a ComponentHandlerConfig for RDS with appropriate settings
func NewRdsOperationsConfig() base.ComponentHandlerConfig {
	config := base.DefaultComponentHandlerConfig(HandlerName, ControllerName)

	// RDS operations typically take longer than Helm operations
	// Adjust timeouts to account for database creation/modification times
	config.DefaultRequeue = 30 * time.Second     // RDS operations are slower
	config.StatusCheckRequeue = 15 * time.Second // Check database status less frequently
	config.ErrorRequeue = 30 * time.Second       // Give more time for transient errors

	return config
}

// Helper methods for RDS operations

// successResult creates an OperationResult for successful operations
func (r *RdsOperations) successResult() *base.OperationResult {
	updatedStatus, _ := json.Marshal(r.status)
	return &base.OperationResult{
		UpdatedStatus: updatedStatus,
		Success:       true,
	}
}

// errorResult creates an OperationResult for failed operations with error details
func (r *RdsOperations) errorResult(err error) *base.OperationResult {
	updatedStatus, _ := json.Marshal(r.status)
	return &base.OperationResult{
		UpdatedStatus:  updatedStatus,
		Success:        false,
		OperationError: err,
	}
}

// pendingResult creates an OperationResult for operations still in progress
func (r *RdsOperations) pendingResult() *base.OperationResult {
	updatedStatus, _ := json.Marshal(r.status)
	return &base.OperationResult{
		UpdatedStatus: updatedStatus,
		Success:       false,
	}
}

// checkInstanceExists checks if an RDS instance already exists
func (r *RdsOperations) checkInstanceExists(ctx context.Context, instanceID string) (bool, error) {
	input := &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: aws.String(instanceID),
	}

	_, err := r.rdsClient.DescribeDBInstances(ctx, input)
	if err != nil {
		if r.isInstanceNotFoundError(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check instance existence: %w", err)
	}

	return true, nil
}

// buildCreateDBInstanceInput constructs the CreateDBInstanceInput from RDS configuration
func (r *RdsOperations) buildCreateDBInstanceInput(config *RdsConfig, instanceID string) *rds.CreateDBInstanceInput {
	input := &rds.CreateDBInstanceInput{
		DBInstanceIdentifier: aws.String(instanceID),
		DBInstanceClass:      aws.String(config.InstanceClass),
		Engine:               aws.String(config.DatabaseEngine),
		EngineVersion:        aws.String(config.EngineVersion),
		AllocatedStorage:     aws.Int32(config.AllocatedStorage),
		MasterUsername:       aws.String(config.MasterUsername),
		MasterUserPassword:   aws.String(config.MasterPassword),
		DBName:               aws.String(config.DatabaseName),
	}

	// Optional storage configuration
	if config.StorageType != "" {
		input.StorageType = aws.String(config.StorageType)
	}
	if config.StorageEncrypted != nil {
		input.StorageEncrypted = aws.Bool(*config.StorageEncrypted)
	}
	if config.KmsKeyId != "" {
		input.KmsKeyId = aws.String(config.KmsKeyId)
	}

	// Optional networking configuration
	if len(config.VpcSecurityGroupIds) > 0 {
		input.VpcSecurityGroupIds = config.VpcSecurityGroupIds
	}
	if config.SubnetGroupName != "" {
		input.DBSubnetGroupName = aws.String(config.SubnetGroupName)
	}
	if config.PubliclyAccessible != nil {
		input.PubliclyAccessible = aws.Bool(*config.PubliclyAccessible)
	}
	if config.Port != nil {
		input.Port = aws.Int32(*config.Port)
	}

	// Optional backup configuration
	if config.BackupRetentionPeriod != nil {
		input.BackupRetentionPeriod = aws.Int32(*config.BackupRetentionPeriod)
	}
	if config.PreferredBackupWindow != "" {
		input.PreferredBackupWindow = aws.String(config.PreferredBackupWindow)
	}

	// Optional maintenance configuration
	if config.PreferredMaintenanceWindow != "" {
		input.PreferredMaintenanceWindow = aws.String(config.PreferredMaintenanceWindow)
	}
	if config.AutoMinorVersionUpgrade != nil {
		input.AutoMinorVersionUpgrade = aws.Bool(*config.AutoMinorVersionUpgrade)
	}

	// Optional performance configuration
	if config.MultiAZ != nil {
		input.MultiAZ = aws.Bool(*config.MultiAZ)
	}
	if config.PerformanceInsightsEnabled != nil {
		input.EnablePerformanceInsights = aws.Bool(*config.PerformanceInsightsEnabled)
	}
	if config.MonitoringInterval != nil && *config.MonitoringInterval > 0 {
		input.MonitoringInterval = aws.Int32(*config.MonitoringInterval)
	}

	// Deletion protection
	if config.DeletionProtection != nil {
		input.DeletionProtection = aws.Bool(*config.DeletionProtection)
	}

	return input
}

// handleOperationError creates a standardized error response for RDS operations
func (r *RdsOperations) handleOperationError(ctx context.Context, message string, err error) (*base.OperationResult, error) {
	log := logf.FromContext(ctx)

	// Update status with error information
	r.status.LastError = err.Error()
	r.status.LastErrorTime = time.Now().Format(time.RFC3339)

	fullError := fmt.Errorf("%s: %w", message, err)
	log.Error(fullError, "RDS operation failed")

	// Check if this is a transient error that should be retried
	if r.isTransientError(err) {
		return r.pendingResult(), fullError // Return error to trigger retry
	}

	// Permanent error - don't retry
	return r.errorResult(fullError), nil
}

// isInstanceNotFoundError checks if the error indicates the RDS instance was not found
func (r *RdsOperations) isInstanceNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "DBInstanceNotFound") ||
		strings.Contains(err.Error(), "does not exist")
}

// isTransientError determines if an error is transient and should be retried
func (r *RdsOperations) isTransientError(err error) bool {
	if err == nil {
		return false
	}

	errorStr := err.Error()
	transientErrors := []string{
		"RequestTimeout",
		"ThrottlingException",
		"ServiceUnavailable",
		"InternalError",
		"network",
		"timeout",
		"connection",
	}

	for _, transientErr := range transientErrors {
		if strings.Contains(strings.ToLower(errorStr), strings.ToLower(transientErr)) {
			return true
		}
	}

	return false
}
