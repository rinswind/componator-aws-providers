// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package iampolicy

import (
	"context"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	corev1beta1 "github.com/rinswind/componator/api/core/v1beta1"
	"github.com/rinswind/componator/componentkit/functional"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	DefaultProviderName = "iam-policy"
)

// Register registers the iam-policy Component provider with the controller manager.
//
// The providerName parameter specifies the unique name used for Component claiming.
// Pass empty string to use the default "iam-policy". For setkit embedding, use a
// prefixed name (e.g., "wordpress-iam-policy") to avoid conflicts with other providers.
//
// Provider names must be unique across all providers in the cluster. Multiple providers
// with the same name will conflict during Component claiming.
//
// Initializes AWS IAM client using the default credential chain
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

	iamClient = iam.NewFromConfig(cfg)

	// Log client initialization
	log := logf.Log.WithName("iam-policy")
	log.Info("Initialized AWS IAM client", "region", cfg.Region)

	// Register with functional API
	return functional.NewBuilder[IamPolicyConfig, IamPolicyStatus](providerName).
		WithApply(applyAction).
		WithApplyCheck(checkApplied).
		WithDelete(deleteAction).
		WithDeleteCheck(checkDeleted).
		Register(mgr)
}
