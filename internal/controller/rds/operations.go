// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package rds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/rinswind/deployment-operator/componentkit/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// HandlerName is the identifier for this RDS handler
	HandlerName = "rds"
)

// rdsErrorClassifier wraps the AWS SDK retry logic for use with result builder utilities.
// This allows RDS handler to use the generic result builders while maintaining AWS-specific error classification.
var rdsErrorClassifier = controller.ErrorClassifier(isRetryable)

// RdsOperationsFactory implements the ComponentOperationsFactory interface for RDS deployments.
// This factory creates stateful RdsOperations instances with pre-parsed configuration,
// eliminating repeated configuration parsing during reconciliation loops.
type RdsOperationsFactory struct{}

// NewOperations creates a new stateful RdsOperations instance with pre-parsed configuration and status.
// This method is called once per reconciliation loop to eliminate repeated configuration parsing.
//
// The returned RdsOperations instance maintains the parsed configuration and status and can be used
// throughout the reconciliation loop without re-parsing the same configuration multiple times.
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

	// Initialize AWS configuration for the specified region
	// Disable AWS SDK retries since we handle retries via controller requeue
	awsConfig, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(rdsConfig.Region),
		awsconfig.WithRetryMaxAttempts(1), // Disable retries - controller handles requeue
	)
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
func NewRdsOperationsConfig() controller.ComponentReconcilerConfig {
	config := controller.DefaultComponentReconcilerConfig(HandlerName)

	// RDS operations typically take longer than Helm operations
	// Adjust timeouts to account for database creation/modification times
	config.DefaultRequeue = 30 * time.Second     // RDS operations are slower
	config.StatusCheckRequeue = 15 * time.Second // Check database status less frequently
	config.ErrorRequeue = 30 * time.Second       // Give more time for transient errors

	return config
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
