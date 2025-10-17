// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package configreader

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rinswind/deployment-operator/componentkit/controller"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

// actionSuccessResult creates an ActionResult for successful Deploy/Delete operations
func (o *ConfigReaderOperations) actionSuccessResult() (*controller.ActionResult, error) {
	updatedStatus, _ := json.Marshal(o.status)
	return &controller.ActionResult{
		UpdatedStatus: updatedStatus,
	}, nil
}

// actionFailureResult creates an ActionResult for permanent failures in Deploy operations
func (o *ConfigReaderOperations) actionFailureResult(err error) (*controller.ActionResult, error) {
	updatedStatus, _ := json.Marshal(o.status)
	return &controller.ActionResult{
		UpdatedStatus:    updatedStatus,
		PermanentFailure: err,
	}, nil
}

// newActionResultForError creates a standardized error response for ConfigReader action operations.
// Uses Kubernetes apierrors classification to distinguish retryable errors (network, timeouts,
// rate limiting) from permanent errors (missing ConfigMap, RBAC issues, key not found).
func (o *ConfigReaderOperations) newActionResultForError(err error) (*controller.ActionResult, error) {
	updatedStatus, _ := json.Marshal(o.status)

	// Check if this error should be retried
	if isRetryable(err) {
		return &controller.ActionResult{UpdatedStatus: updatedStatus}, err
	}

	// Permanent error - missing ConfigMap, RBAC issue, or key not found
	return o.actionFailureResult(err)
}

// checkCompleteResult creates a CheckResult for completed check operations
func (o *ConfigReaderOperations) checkCompleteResult() (*controller.CheckResult, error) {
	updatedStatus, _ := json.Marshal(o.status)
	return &controller.CheckResult{
		UpdatedStatus: updatedStatus,
		Complete:      true,
	}, nil
}

// isRetryable determines if a Kubernetes API error is retryable.
// Uses apierrors classification similar to how RDS handler uses AWS SDK retry classification.
//
// Retryable errors include network issues, timeouts, rate limiting, and temporary server problems.
// Permanent errors include validation failures, authorization issues, and malformed requests.
func isRetryable(err error) bool {
	return apierrors.IsTimeout(err) ||
		apierrors.IsServerTimeout(err) ||
		apierrors.IsTooManyRequests(err) ||
		apierrors.IsServiceUnavailable(err) ||
		apierrors.IsInternalError(err) ||
		apierrors.IsConflict(err) ||
		apierrors.IsResourceExpired(err)
}
