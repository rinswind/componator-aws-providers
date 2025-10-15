// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rinswind/deployment-operator/componentkit/controller"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/dynamic"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// ManifestOperationsFactory implements the ComponentOperationsFactory interface for Manifest deployments.
type ManifestOperationsFactory struct {
	dynamicClient dynamic.Interface
	mapper        meta.RESTMapper
}

// NewManifestOperationsFactory creates a new ManifestOperationsFactory with the required clients.
// The dynamicClient is used for applying arbitrary Kubernetes resources.
// The mapper is used for GVK to GVR conversion.
func NewManifestOperationsFactory(dynamicClient dynamic.Interface, mapper meta.RESTMapper) *ManifestOperationsFactory {
	return &ManifestOperationsFactory{
		dynamicClient: dynamicClient,
		mapper:        mapper,
	}
}

// NewOperations creates a new ManifestOperations instance with pre-parsed configuration and status.
func (f *ManifestOperationsFactory) NewOperations(
	ctx context.Context, rawConfig json.RawMessage, rawStatus json.RawMessage) (controller.ComponentOperations, error) {

	// Parse the manifest config
	config, err := resolveManifestConfig(ctx, rawConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to parse manifest configuration: %w", err)
	}

	log := logf.FromContext(ctx).WithValues("manifestCount", len(config.Manifests))
	log.V(1).Info("Creating manifest operations")

	// Parse the manifest status
	status, err := resolveManifestStatus(ctx, rawStatus)
	if err != nil {
		return nil, fmt.Errorf("failed to parse manifest status: %w", err)
	}

	return &ManifestOperations{
		dynamicClient: f.dynamicClient,
		mapper:        f.mapper,
		config:        config,
		status:        status,
	}, nil
}

// ManifestOperations implements the ComponentOperations interface for Manifest-based deployments.
// This struct provides all Manifest-specific deployment, status checking, and deletion operations
// with pre-parsed configuration maintained throughout the reconciliation loop.
type ManifestOperations struct {
	dynamicClient dynamic.Interface
	mapper        meta.RESTMapper
	config        *ManifestConfig
	status        *ManifestStatus
}

// successResult creates an OperationResult for successful operations
func (m *ManifestOperations) successResult() *controller.OperationResult {
	updatedStatus, _ := json.Marshal(m.status)
	return &controller.OperationResult{
		UpdatedStatus: updatedStatus,
		Success:       true,
	}
}

// errorResult creates an OperationResult for failed operations with error details
func (m *ManifestOperations) errorResult(err error) *controller.OperationResult {
	updatedStatus, _ := json.Marshal(m.status)
	return &controller.OperationResult{
		UpdatedStatus:  updatedStatus,
		Success:        false,
		OperationError: err,
	}
}

// pendingResult creates an OperationResult for operations still in progress
func (m *ManifestOperations) pendingResult() *controller.OperationResult {
	updatedStatus, _ := json.Marshal(m.status)
	return &controller.OperationResult{
		UpdatedStatus: updatedStatus,
		Success:       false,
	}
}
