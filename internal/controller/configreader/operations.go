// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package configreader

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rinswind/deployment-operator/componentkit/controller"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ConfigReaderOperationsFactory implements the ComponentOperationsFactory interface for config-reader components.
type ConfigReaderOperationsFactory struct {
	// apiReader provides direct API access bypassing cache for fresh ConfigMap reads
	apiReader client.Reader
}

// NewConfigReaderOperationsFactory creates a new ConfigReaderOperationsFactory.
// apiReader should be the manager's APIReader for non-cached ConfigMap access.
func NewConfigReaderOperationsFactory(apiReader client.Reader) *ConfigReaderOperationsFactory {
	return &ConfigReaderOperationsFactory{
		apiReader: apiReader,
	}
}

// NewOperations creates a new ConfigReaderOperations instance with pre-parsed configuration and status.
func (f *ConfigReaderOperationsFactory) NewOperations(
	ctx context.Context, rawConfig json.RawMessage, rawStatus json.RawMessage) (controller.ComponentOperations, error) {

	// Parse the config-reader config
	config, err := resolveConfigReaderConfig(ctx, rawConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config-reader configuration: %w", err)
	}

	// Parse the config-reader status
	status, err := resolveConfigReaderStatus(ctx, rawStatus)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config-reader status: %w", err)
	}

	return &ConfigReaderOperations{
		config:    config,
		status:    status,
		apiReader: f.apiReader,
	}, nil
}

// ConfigReaderOperations implements the ComponentOperations interface for config-reader components.
type ConfigReaderOperations struct {
	config    *ConfigReaderConfig
	status    ConfigReaderStatus
	apiReader client.Reader
}

// successResult creates an OperationResult for successful operations.
// Returns the result and nil error, matching the ComponentOperations method signatures.
func (o *ConfigReaderOperations) successResult() (*controller.OperationResult, error) {
	updatedStatus, _ := json.Marshal(o.status)
	return &controller.OperationResult{
		UpdatedStatus: updatedStatus,
		Success:       true,
	}, nil
}

// errorResult creates a standardized error response for ConfigReader operations.
// Uses Kubernetes apierrors classification to distinguish transient errors (network, timeouts,
// rate limiting) from permanent errors (missing ConfigMap, RBAC issues, key not found).
func (o *ConfigReaderOperations) errorResult(ctx context.Context, err error) (*controller.OperationResult, error) {
	log := logf.FromContext(ctx)

	log.Error(err, "ConfigReader operation failed")

	updatedStatus, _ := json.Marshal(o.status)

	// Check if this is a transient error that should be retried
	if isTransientError(err) {
		// Transient Kubernetes API error - return error to trigger retry
		return &controller.OperationResult{
			UpdatedStatus: updatedStatus,
			Success:       false,
		}, err
	}

	// Permanent error - missing ConfigMap, RBAC issue, or key not found
	return &controller.OperationResult{
		UpdatedStatus:  updatedStatus,
		Success:        false,
		OperationError: err,
	}, nil
}

// isTransientError determines if a Kubernetes API error is transient and should be retried.
// Uses apierrors classification to handle ConfigMap access errors appropriately.
//
// Transient errors include network issues, timeouts, rate limiting, and temporary server problems.
// Permanent errors include validation failures, authorization issues, and missing resources.
func isTransientError(err error) bool {
	if err == nil {
		return false
	}

	// Transient errors that should be retried:

	// Network and timeout errors - temporary connectivity issues
	if apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) {
		return true
	}

	// Rate limiting - server is overloaded but request is valid
	if apierrors.IsTooManyRequests(err) {
		return true
	}

	// Server errors - temporary server issues
	if apierrors.IsServiceUnavailable(err) || apierrors.IsInternalError(err) {
		return true
	}

	// Optimistic concurrency conflicts - safe to retry with fresh data
	if apierrors.IsConflict(err) {
		return true
	}

	// Resource version expired - need to refetch and retry
	if apierrors.IsResourceExpired(err) {
		return true
	}

	// All other errors are considered permanent:
	// - IsNotFound() - ConfigMap doesn't exist (need user to create it)
	// - IsForbidden() - RBAC issue (need role/binding)
	// - IsUnauthorized() - authentication failed (need credentials)
	// - IsBadRequest() - malformed request (won't fix itself)
	// - Custom "key not found" errors - configuration problem

	return false
}

