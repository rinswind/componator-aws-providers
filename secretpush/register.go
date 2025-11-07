// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package secretpush

import (
	"context"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	corev1beta1 "github.com/rinswind/componator/api/core/v1beta1"
	"github.com/rinswind/componator/componentkit/functional"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	DefaultProviderName = "secret-push"
)

// Register registers the secret-push Component provider with the controller manager.
//
// The providerName parameter specifies the unique name used for Component claiming.
// Pass empty string to use the default "secret-push". For setkit embedding, use a
// prefixed name (e.g., "wordpress-secret-push") to avoid conflicts with other providers.
//
// Provider names must be unique across all providers in the cluster. Multiple providers
// with the same name will conflict during Component claiming.
//
// Initializes AWS Secrets Manager client using the default credential chain
// (environment variables, EC2 instance metadata, etc.).
func Register(mgr ctrl.Manager, providerName string) error {
	// Ensure required schemes are registered (safe to call multiple times)
	if err := clientgoscheme.AddToScheme(mgr.GetScheme()); err != nil {
		return err
	}
	if err := corev1beta1.AddToScheme(mgr.GetScheme()); err != nil {
		return err
	}

	// Use default provider name if not specified
	if providerName == "" {
		providerName = DefaultProviderName
	}

	// Load AWS config with default chain (uses AWS_REGION, EC2 metadata, etc.)
	// Disable retries - controller handles requeue
	// Use WithEC2IMDSRegion to auto-detect region from EC2 metadata when in EKS
	cfg, err := awsconfig.LoadDefaultConfig(
		context.Background(),
		awsconfig.WithRetryMaxAttempts(1),
		awsconfig.WithEC2IMDSRegion(),
	)
	if err != nil {
		return fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	awsConfig = cfg
	smClient = secretsmanager.NewFromConfig(cfg)

	// Log client initialization
	log := logf.Log.WithName("secret-push")
	log.Info("Initialized AWS Secrets Manager client", "region", cfg.Region)

	// Register with functional API (immediate operations - no progress checks)
	return functional.NewBuilder[SecretPushSpec, SecretPushStatus](providerName).
		WithApply(applyAction).
		WithDelete(deleteAction).
		Register(mgr)
}
