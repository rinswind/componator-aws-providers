// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package configreader

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rinswind/deployment-operator/componentkit/controller"
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

// successResult creates an OperationResult for successful operations
func (o *ConfigReaderOperations) successResult() *controller.OperationResult {
	updatedStatus, _ := json.Marshal(o.status)
	return &controller.OperationResult{
		UpdatedStatus: updatedStatus,
		Success:       true,
	}
}

// errorResult creates an OperationResult for failed operations with error details
func (o *ConfigReaderOperations) errorResult(err error) *controller.OperationResult {
	updatedStatus, _ := json.Marshal(o.status)
	return &controller.OperationResult{
		UpdatedStatus:  updatedStatus,
		Success:        false,
		OperationError: err,
	}
}
