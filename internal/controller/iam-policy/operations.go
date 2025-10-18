// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package iampolicy

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
	// HandlerName is the identifier for this IAM policy handler
	HandlerName = "iam-policy"
)

// iamErrorClassifier wraps the AWS SDK retry logic for use with result builder utilities.
// This allows IAM policy handler to use the generic result builders while maintaining AWS-specific error classification.
var iamErrorClassifier = controller.ErrorClassifier(isRetryable)

// IamPolicyOperationsFactory implements the ComponentOperationsFactory interface for IAM policy deployments.
// This factory creates stateful IamPolicyOperations instances with pre-parsed configuration,
// eliminating repeated configuration parsing during reconciliation loops.
type IamPolicyOperationsFactory struct{}

// NewOperations creates a new stateful IamPolicyOperations instance with pre-parsed configuration and status.
// This method is called once per reconciliation loop to eliminate repeated configuration parsing.
//
// The returned IamPolicyOperations instance maintains the parsed configuration and status and can be used
// throughout the reconciliation loop without re-parsing the same configuration multiple times.
func (f *IamPolicyOperationsFactory) NewOperations(
	ctx context.Context, config json.RawMessage, currentStatus json.RawMessage) (controller.ComponentOperations, error) {

	log := logf.FromContext(ctx)

	// Parse configuration once for this reconciliation loop
	iamConfig, err := resolveIamPolicyConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse iam-policy configuration: %w", err)
	}

	// Parse existing handler status
	status, err := resolveIamPolicyStatus(ctx, currentStatus)
	if err != nil {
		return nil, fmt.Errorf("failed to parse iam-policy status: %w", err)
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

	log.V(1).Info("Created IAM policy operations with AWS client",
		"policyName", iamConfig.PolicyName,
		"path", iamConfig.Path)

	// Return stateful operations instance with pre-parsed configuration and status
	return &IamPolicyOperations{
		config:    iamConfig,
		status:    status,
		iamClient: iamClient,
		awsConfig: awsConfig,
	}, nil
}

// IamPolicyOperations implements the ComponentOperations interface for IAM policy-based deployments.
// This struct provides all IAM-specific policy creation, versioning, and deletion operations
// for managing AWS IAM managed policies through the AWS SDK with pre-parsed configuration.
//
// This is a stateful operations instance created by IamPolicyOperationsFactory that eliminates
// repeated configuration parsing by maintaining parsed configuration state.
type IamPolicyOperations struct {
	// config holds the pre-parsed IAM policy configuration for this reconciliation loop
	config *IamPolicyConfig

	// status holds the pre-parsed IAM policy status for this reconciliation loop
	status *IamPolicyStatus

	// AWS SDK clients for IAM operations
	iamClient *iam.Client
	awsConfig aws.Config
}

// NewIamPolicyOperationsFactory creates a new IamPolicyOperationsFactory instance
func NewIamPolicyOperationsFactory() *IamPolicyOperationsFactory {
	return &IamPolicyOperationsFactory{}
}

// getPolicyByName retrieves policy by name (searches by path and name)
func (op *IamPolicyOperations) getPolicyByName(ctx context.Context, policyName, path string) (*types.Policy, error) {
	// Construct expected ARN from account and policy name
	// We need to get the policy using GetPolicy with constructed ARN
	// First, try to list policies to find a match
	input := &iam.ListPoliciesInput{
		Scope:      types.PolicyScopeTypeLocal,
		PathPrefix: aws.String(path),
	}

	output, err := op.iamClient.ListPolicies(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to list policies: %w", err)
	}

	// Find policy with matching name
	for i := range output.Policies {
		policy := &output.Policies[i]
		if aws.ToString(policy.PolicyName) == policyName {
			return policy, nil
		}
	}

	return nil, nil // Not found
}

// getPolicyByArn retrieves policy by ARN
func (op *IamPolicyOperations) getPolicyByArn(ctx context.Context, arn string) (*types.Policy, error) {
	input := &iam.GetPolicyInput{
		PolicyArn: aws.String(arn),
	}

	output, err := op.iamClient.GetPolicy(ctx, input)
	if err == nil {
		return output.Policy, nil
	}

	// Check if policy not found
	if isNotFoundError(err) {
		return nil, nil
	}

	return nil, fmt.Errorf("failed to get policy: %w", err)
}

// isNotFoundError checks if error indicates policy not found
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
