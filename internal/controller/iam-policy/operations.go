// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package iampolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/rinswind/deployment-operator/componentkit/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// HandlerName is the identifier for this IAM policy handler
	HandlerName = "iam-policy"

	// ControllerName is the name used for controller registration
	ControllerName = "iam-policy-component"
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
func (f *IamPolicyOperationsFactory) NewOperations(ctx context.Context, config json.RawMessage, currentStatus json.RawMessage) (controller.ComponentOperations, error) {
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

	// Initialize AWS configuration for the specified region
	// Disable AWS SDK retries since we handle retries via controller requeue
	awsConfig, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(iamConfig.Region),
		awsconfig.WithRetryMaxAttempts(1), // Disable retries - controller handles requeue
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS configuration for region %s: %w", iamConfig.Region, err)
	}

	// Create IAM client with the configured region
	iamClient := iam.NewFromConfig(awsConfig)

	log.V(1).Info("Created IAM policy operations with AWS client",
		"region", iamConfig.Region,
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

// NewIamPolicyOperationsConfig creates a ComponentReconcilerConfig for IAM policy with appropriate settings
func NewIamPolicyOperationsConfig() controller.ComponentReconcilerConfig {
	config := controller.DefaultComponentReconcilerConfig(HandlerName)

	// IAM operations are typically fast but may have API throttling
	// Adjust timeouts appropriately
	config.DefaultRequeue = 15 * time.Second     // IAM operations are generally fast
	config.StatusCheckRequeue = 10 * time.Second // Check policy status frequently
	config.ErrorRequeue = 30 * time.Second       // Give more time for transient errors like throttling

	return config
}

// Deploy implements the deployment operation - stub for Phase 1
func (op *IamPolicyOperations) Deploy(ctx context.Context) (*controller.ActionResult, error) {
	return controller.ActionFailure(op.status, fmt.Errorf("not implemented"))
}

// CheckDeployment checks if deployment is complete and ready - stub for Phase 1
func (op *IamPolicyOperations) CheckDeployment(ctx context.Context) (*controller.CheckResult, error) {
	return controller.CheckFailure(op.status, fmt.Errorf("not implemented"))
}

// Delete implements deletion operations - stub for Phase 1
func (op *IamPolicyOperations) Delete(ctx context.Context) (*controller.ActionResult, error) {
	return controller.ActionFailure(op.status, fmt.Errorf("not implemented"))
}

// CheckDeletion verifies deletion is complete - stub for Phase 1
func (op *IamPolicyOperations) CheckDeletion(ctx context.Context) (*controller.CheckResult, error) {
	return controller.CheckFailure(op.status, fmt.Errorf("not implemented"))
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
