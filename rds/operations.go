// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package rds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/rinswind/componator/componentkit/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// rdsErrorClassifier wraps the AWS SDK retry logic for use with result builder utilities.
// This allows RDS handler to use the generic result builders while maintaining AWS-specific error classification.
var rdsErrorClassifier = controller.ErrorClassifier(isRetryable)

// RdsOperationsFactory implements the ComponentOperationsFactory interface for RDS deployments.
// This factory creates stateful RdsOperations instances with pre-parsed configuration,
// eliminating repeated configuration parsing during reconciliation loops.
// It maintains a single AWS RDS client created at factory initialization.
type RdsOperationsFactory struct {
	rdsClient *rds.Client
	awsConfig aws.Config
}

// NewOperations creates a new stateful RdsOperations instance with pre-parsed configuration and status.
// This method is called once per reconciliation loop to eliminate repeated configuration parsing.
// Uses the pre-initialized AWS client from the factory.
func (f *RdsOperationsFactory) NewOperations(ctx context.Context, config json.RawMessage, currentStatus json.RawMessage) (controller.ComponentOperations, error) {
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

	log.V(1).Info("Created RDS operations",
		"region", f.awsConfig.Region,
		"databaseEngine", rdsConfig.DatabaseEngine,
		"instanceClass", rdsConfig.InstanceClass)

	// Return stateful operations instance with pre-parsed configuration and status
	return &RdsOperations{
		config:    rdsConfig,
		status:    status,
		rdsClient: f.rdsClient,
		awsConfig: f.awsConfig,
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
// with a pre-initialized AWS RDS client.
// This is called once during controller setup, not during reconciliation.
func NewRdsOperationsFactory() *RdsOperationsFactory {
	// Use background context for AWS SDK initialization during controller setup
	// This is safe because:
	// 1. Controller setup happens once at startup, not during reconciliation
	// 2. AWS SDK uses context for credential loading and metadata calls
	// 3. These operations complete quickly during initialization
	ctx := context.Background()

	// Load AWS config with default chain (uses AWS_REGION, EC2 metadata, etc.)
	// Disable retries - controller handles requeue
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRetryMaxAttempts(1),
	)
	if err != nil {
		// Fatal error - controller cannot function without AWS credentials
		panic(fmt.Sprintf("failed to load AWS configuration: %v", err))
	}

	// Create RDS client once
	rdsClient := rds.NewFromConfig(cfg)

	// Log client initialization (using background context logger)
	log := logf.Log.WithName("rds-factory")
	log.Info("Initialized AWS RDS client",
		"region", cfg.Region)

	return &RdsOperationsFactory{
		rdsClient: rdsClient,
		awsConfig: cfg,
	}
}

// Helper methods for RDS operations

// updateStatus updates RdsStatus fields from AWS DBInstance data
// This eliminates repetitive field-by-field copying across all RDS operations
func (r *RdsOperations) updateStatus(instance *types.DBInstance) {
	r.status.InstanceStatus = stringValue(instance.DBInstanceStatus)
	r.status.InstanceARN = stringValue(instance.DBInstanceArn)
	r.status.Endpoint = endpointAddress(instance.Endpoint)
	r.status.Port = endpointPort(instance.Endpoint)
	r.status.AvailabilityZone = stringValue(instance.AvailabilityZone)

	// Preserve or update managed password secret ARN
	// The ARN is immutable once created, but may be present in DescribeDBInstances response
	if instance.MasterUserSecret != nil && instance.MasterUserSecret.SecretArn != nil {
		r.status.MasterUserSecretArn = *instance.MasterUserSecret.SecretArn
	}
	// If not present in response but already in status, keep existing value (ARN doesn't change)
}

// getInstanceData retrieves RDS instance data, handling not-found cases consistently
func (r *RdsOperations) getInstanceData(ctx context.Context, instanceID string) (*types.DBInstance, error) {
	input := &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: stringPtr(instanceID),
	}

	result, err := r.rdsClient.DescribeDBInstances(ctx, input)
	if err != nil {
		if isInstanceNotFoundError(err) {
			return nil, nil // Instance not found - return nil without error
		}
		return nil, fmt.Errorf("failed to describe RDS instance: %w", err)
	}

	if len(result.DBInstances) == 0 {
		return nil, nil // No instances returned - treat as not found
	}

	return &result.DBInstances[0], nil
}

// isInstanceNotFoundError checks if the error indicates the RDS instance was not found
// Uses AWS SDK error types instead of string matching
func isInstanceNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific AWS RDS error type
	var notFoundErr *types.DBInstanceNotFoundFault
	return errors.As(err, &notFoundErr)
}

// isInstanceAlreadyBeingDeletedError checks if the error indicates the RDS instance is already being deleted
func isInstanceAlreadyBeingDeletedError(err error) bool {
	if err == nil {
		return false
	}

	// Check for InvalidDBInstanceState error when instance is already being deleted
	var invalidStateErr *types.InvalidDBInstanceStateFault
	if errors.As(err, &invalidStateErr) {
		// Check if the error message indicates the instance is already being deleted
		return strings.Contains(strings.ToLower(invalidStateErr.ErrorMessage()), "already being deleted")
	}

	return false
}

// isRetryable determines if an error is retryable.
// Uses AWS SDK's built-in error classification instead of custom logic.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Use AWS SDK's built-in retry classification
	// This handles all AWS API errors, network errors, and HTTP status codes
	for _, checker := range retry.DefaultRetryables {
		if checker.IsErrorRetryable(err) == aws.TrueTernary {
			return true
		}
	}

	return false
}
