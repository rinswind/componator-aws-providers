// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package iamrole

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/rinswind/componator/componentkit/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// iamErrorClassifier wraps the AWS SDK retry logic for use with result builder utilities.
// This allows IAM role handler to use the generic result builders while maintaining AWS-specific error classification.
var iamErrorClassifier = controller.ErrorClassifier(isRetryable)

// IamRoleOperationsFactory implements the ComponentOperationsFactory interface for IAM role deployments.
// This factory creates stateful IamRoleOperations instances with pre-parsed configuration,
// eliminating repeated configuration parsing during reconciliation loops.
// It maintains a single AWS IAM client created at factory initialization.
type IamRoleOperationsFactory struct {
	iamClient *iam.Client
	awsConfig aws.Config
}

// NewOperations creates a new stateful IamRoleOperations instance with pre-parsed configuration and status.
// This method is called once per reconciliation loop to eliminate repeated configuration parsing.
// Uses the pre-initialized AWS client from the factory.
func (f *IamRoleOperationsFactory) NewOperations(
	ctx context.Context, config json.RawMessage, currentStatus json.RawMessage) (controller.ComponentOperations, error) {

	log := logf.FromContext(ctx)

	// Parse configuration once for this reconciliation loop
	iamConfig, err := resolveIamRoleConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse iam-role configuration: %w", err)
	}

	// Parse existing handler status
	status, err := resolveIamRoleStatus(ctx, currentStatus)
	if err != nil {
		return nil, fmt.Errorf("failed to parse iam-role status: %w", err)
	}

	log.V(1).Info("Created IAM role operations",
		"roleName", iamConfig.RoleName,
		"path", iamConfig.Path,
		"policyCount", len(iamConfig.ManagedPolicyArns))

	// Return stateful operations instance with pre-parsed configuration and status
	return &IamRoleOperations{
		config:    iamConfig,
		status:    status,
		iamClient: f.iamClient,
		awsConfig: f.awsConfig,
	}, nil
}

// IamRoleOperations implements the ComponentOperations interface for IAM role-based deployments.
// This struct provides all IAM-specific role creation, trust policy updates, policy attachment management,
// and deletion operations for managing AWS IAM roles through the AWS SDK with pre-parsed configuration.
//
// This is a stateful operations instance created by IamRoleOperationsFactory that eliminates
// repeated configuration parsing by maintaining parsed configuration state.
type IamRoleOperations struct {
	// config holds the pre-parsed IAM role configuration for this reconciliation loop
	config *IamRoleConfig

	// status holds the pre-parsed IAM role status for this reconciliation loop
	status *IamRoleStatus

	// AWS SDK clients for IAM operations
	iamClient *iam.Client
	awsConfig aws.Config
}

// NewIamRoleOperationsFactory creates a new IamRoleOperationsFactory instance
// with a pre-initialized AWS IAM client.
// This is called once during controller setup, not during reconciliation.
func NewIamRoleOperationsFactory() *IamRoleOperationsFactory {
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

	// Create IAM client once (IAM is global, uses region from config chain)
	iamClient := iam.NewFromConfig(cfg)

	// Log client initialization (using background context logger)
	log := logf.Log.WithName("iam-role-factory")
	log.Info("Initialized AWS IAM client",
		"region", cfg.Region)

	return &IamRoleOperationsFactory{
		iamClient: iamClient,
		awsConfig: cfg,
	}
}

// getRoleByName retrieves role by name
func (op *IamRoleOperations) getRoleByName(ctx context.Context, roleName string) (*types.Role, error) {
	input := &iam.GetRoleInput{
		RoleName: aws.String(roleName),
	}

	output, err := op.iamClient.GetRole(ctx, input)
	if err == nil {
		return output.Role, nil
	}

	// Check if role not found
	if isNotFoundError(err) {
		return nil, nil
	}

	return nil, fmt.Errorf("failed to get role: %w", err)
}

// listAttachedPolicies retrieves all managed policies currently attached to the role
func (op *IamRoleOperations) listAttachedPolicies(ctx context.Context, roleName string) ([]string, error) {
	input := &iam.ListAttachedRolePoliciesInput{
		RoleName: aws.String(roleName),
	}

	output, err := op.iamClient.ListAttachedRolePolicies(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to list attached policies: %w", err)
	}

	arns := make([]string, 0, len(output.AttachedPolicies))
	for i := range output.AttachedPolicies {
		arns = append(arns, aws.ToString(output.AttachedPolicies[i].PolicyArn))
	}

	return arns, nil
}

// attachPolicy attaches a managed policy to the role
func (op *IamRoleOperations) attachPolicy(ctx context.Context, roleName, policyArn string) error {
	input := &iam.AttachRolePolicyInput{
		RoleName:  aws.String(roleName),
		PolicyArn: aws.String(policyArn),
	}

	_, err := op.iamClient.AttachRolePolicy(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to attach policy: %w", err)
	}

	return nil
}

// detachPolicy detaches a managed policy from the role
func (op *IamRoleOperations) detachPolicy(ctx context.Context, roleName, policyArn string) error {
	input := &iam.DetachRolePolicyInput{
		RoleName:  aws.String(roleName),
		PolicyArn: aws.String(policyArn),
	}

	_, err := op.iamClient.DetachRolePolicy(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to detach policy: %w", err)
	}

	return nil
}

// isNotFoundError checks if error indicates role not found
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	// Check for IAM NoSuchEntity error
	var notFoundErr *types.NoSuchEntityException
	return errors.As(err, &notFoundErr)
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
