// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package rds

import (
	"context"
	"fmt"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/rinswind/componator/componentkit/functional"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	DefaultProviderName = "rds"
)

// Register registers the rds Component provider with the controller manager.
//
// The providerName parameter specifies the unique name used for Component claiming.
// Pass empty string to use the default "rds". For setkit embedding, use a
// prefixed name (e.g., "wordpress-rds") to avoid conflicts with other providers.
//
// Provider names must be unique across all providers in the cluster. Multiple providers
// with the same name will conflict during Component claiming.
//
// Initializes AWS RDS client using the default credential chain
// (environment variables, EC2 instance metadata, etc.).
func Register(mgr ctrl.Manager, providerName string) error {
	// Use default provider name if not specified
	if providerName == "" {
		providerName = DefaultProviderName
	}

	// Load AWS config with default chain (uses AWS_REGION, EC2 metadata, etc.)
	// Disable retries - controller handles requeue
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRetryMaxAttempts(1))
	if err != nil {
		return fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	rdsClient = rds.NewFromConfig(cfg)

	// Log client initialization
	log := logf.Log.WithName("rds")
	log.Info("Initialized AWS RDS client", "region", cfg.Region)

	// Register with functional API using custom timeouts for RDS operations
	return functional.NewBuilder[RdsConfig, RdsStatus](providerName).
		WithApply(applyAction).
		WithApplyCheck(checkApplied).
		WithDelete(deleteAction).
		WithDeleteCheck(checkDeleted).
		WithErrorRequeue(15 * time.Second).
		WithDefaultRequeue(30 * time.Second).
		WithStatusCheckRequeue(30 * time.Second).
		Register(mgr)
}
