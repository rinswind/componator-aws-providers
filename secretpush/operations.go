// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package secretpush

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/rinswind/componator/componentkit/controller"
	k8stypes "k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// awsErrorClassifier wraps the AWS SDK retry logic for use with result builder utilities.
var awsErrorClassifier = controller.ErrorClassifier(isRetryable)

// SecretPushOperationsFactory implements the ComponentOperationsFactory interface for secret-push deployments.
// This factory creates stateful SecretPushOperations instances with pre-parsed configuration,
// eliminating repeated configuration parsing during reconciliation loops.
// It maintains a single AWS Secrets Manager client created at factory initialization.
type SecretPushOperationsFactory struct {
	smClient  *secretsmanager.Client
	awsConfig aws.Config
}

// NewOperations creates a new stateful SecretPushOperations instance with pre-parsed configuration and status.
// This method is called once per reconciliation loop to eliminate repeated configuration parsing.
// Uses the pre-initialized AWS client from the factory.
func (f *SecretPushOperationsFactory) NewOperations(
	ctx context.Context,
	name k8stypes.NamespacedName,
	config json.RawMessage,
	currentStatus json.RawMessage) (controller.ComponentOperations, error) {

	log := logf.FromContext(ctx)

	// Parse configuration once for this reconciliation loop
	pushConfig, err := resolveSecretPushConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse secret-push configuration: %w", err)
	}

	// Parse existing handler status
	status, err := resolveSecretPushStatus(ctx, currentStatus)
	if err != nil {
		return nil, fmt.Errorf("failed to parse secret-push status: %w", err)
	}

	log.V(1).Info("Created secret-push operations",
		"secretName", pushConfig.SecretName,
		"region", f.awsConfig.Region,
		"fieldCount", len(pushConfig.Fields))

	// Return stateful operations instance with pre-parsed configuration and status
	return &SecretPushOperations{
		config:    pushConfig,
		status:    status,
		smClient:  f.smClient,
		awsConfig: f.awsConfig,
	}, nil
}

// SecretPushOperations implements the ComponentOperations interface for secret-push deployments.
// This struct provides all secret generation and AWS Secrets Manager operations
// for managing secrets through the AWS SDK with pre-parsed configuration.
type SecretPushOperations struct {
	// config holds the pre-parsed secret-push configuration for this reconciliation loop
	config *SecretPushConfig

	// status holds the pre-parsed secret-push status for this reconciliation loop
	status *SecretPushStatus

	// Field counts from latest buildSecretData call
	generatedCount int
	staticCount    int

	// AWS SDK clients for Secrets Manager operations
	smClient  *secretsmanager.Client
	awsConfig aws.Config
}

// NewSecretPushOperationsFactory creates a new SecretPushOperationsFactory instance
// with a pre-initialized AWS Secrets Manager client.
// This is called once during controller setup, not during reconciliation.
func NewSecretPushOperationsFactory() *SecretPushOperationsFactory {
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

	// Create Secrets Manager client once
	smClient := secretsmanager.NewFromConfig(cfg)

	// Log client initialization (using background context logger)
	log := logf.Log.WithName("secret-push-factory")
	log.Info("Initialized AWS Secrets Manager client",
		"region", cfg.Region)

	return &SecretPushOperationsFactory{
		smClient:  smClient,
		awsConfig: cfg,
	}
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
