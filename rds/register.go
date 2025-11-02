// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package rds

import (
	"context"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	componentkit "github.com/rinswind/componator/componentkit/controller"
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

	// Register with functional API (no health check)
	// TODO: RegisterFunc doesn't support passing reconciler config (retry/timeout settings).
	// We previously used:
	//   config.ErrorRequeue = 15 * time.Second
	//   config.DefaultRequeue = 30 * time.Second
	//   config.StatusCheckRequeue = 30 * time.Second
	// Need to update componentkit.RegisterFunc to accept optional config parameter.
	return componentkit.RegisterFunc(mgr, providerName, applyAction, checkApplied, deleteAction, checkDeleted, nil)
}
