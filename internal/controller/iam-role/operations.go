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
	"github.com/rinswind/deployment-operator/componentkit/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// HandlerName is the identifier for this IAM role handler
	HandlerName = "iam-role"
)

// iamErrorClassifier wraps the AWS SDK retry logic for use with result builder utilities.
// This allows IAM role handler to use the generic result builders while maintaining AWS-specific error classification.
var iamErrorClassifier = controller.ErrorClassifier(isRetryable)

// IamRoleOperationsFactory implements the ComponentOperationsFactory interface for IAM role deployments.
// This factory creates stateful IamRoleOperations instances with pre-parsed configuration,
// eliminating repeated configuration parsing during reconciliation loops.
type IamRoleOperationsFactory struct{}

// NewOperations creates a new stateful IamRoleOperations instance with pre-parsed configuration and status.
// This method is called once per reconciliation loop to eliminate repeated configuration parsing.
//
// The returned IamRoleOperations instance maintains the parsed configuration and status and can be used
// throughout the reconciliation loop without re-parsing the same configuration multiple times.
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

	// Initialize AWS configuration using default config chain
	// Disable AWS SDK retries since we handle retries via controller requeue
	awsConfig, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRetryMaxAttempts(1), // Disable retries - controller handles requeue
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	// Create IAM client (IAM is global, uses region from config chain)
	iamClient := iam.NewFromConfig(awsConfig)

	log.V(1).Info("Created IAM role operations with AWS client",
		"roleName", iamConfig.RoleName,
		"path", iamConfig.Path,
		"policyCount", len(iamConfig.ManagedPolicyArns))

	// Return stateful operations instance with pre-parsed configuration and status
	return &IamRoleOperations{
		config:    iamConfig,
		status:    status,
		iamClient: iamClient,
		awsConfig: awsConfig,
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
func NewIamRoleOperationsFactory() *IamRoleOperationsFactory {
	return &IamRoleOperationsFactory{}
}

// getRoleByName retrieves role by name
func (op *IamRoleOperations) getRoleByName(ctx context.Context) (*types.Role, error) {
	input := &iam.GetRoleInput{
		RoleName: aws.String(op.config.RoleName),
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
func (op *IamRoleOperations) listAttachedPolicies(ctx context.Context) ([]string, error) {
	input := &iam.ListAttachedRolePoliciesInput{
		RoleName: aws.String(op.config.RoleName),
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
func (op *IamRoleOperations) attachPolicy(ctx context.Context, policyArn string) error {
	input := &iam.AttachRolePolicyInput{
		RoleName:  aws.String(op.config.RoleName),
		PolicyArn: aws.String(policyArn),
	}

	_, err := op.iamClient.AttachRolePolicy(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to attach policy: %w", err)
	}

	return nil
}

// detachPolicy detaches a managed policy from the role
func (op *IamRoleOperations) detachPolicy(ctx context.Context, policyArn string) error {
	input := &iam.DetachRolePolicyInput{
		RoleName:  aws.String(op.config.RoleName),
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
