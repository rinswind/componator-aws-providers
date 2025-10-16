// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rinswind/deployment-operator/componentkit/controller"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

// marshalStatus marshals the handler status and logs any errors.
// Returns the marshaled status or nil if marshaling fails.
func (m *ManifestOperations) marshalStatus(ctx context.Context) json.RawMessage {
	updatedStatus, err := json.Marshal(m.status)
	if err != nil {
		// This should never happen with our simple status struct, but log it if it does
		logf.FromContext(ctx).Error(err, "Failed to marshal handler status")
		return nil
	}
	return updatedStatus
}

// successResult creates an OperationResult for successful operations.
// Returns the result and nil error, matching the ComponentOperations method signatures.
func (m *ManifestOperations) successResult(ctx context.Context) (*controller.OperationResult, error) {
	return &controller.OperationResult{
		UpdatedStatus: m.marshalStatus(ctx),
		Success:       true,
	}, nil
}

// errorResult creates an OperationResult for failed operations with error details.
// Returns the result and nil error, matching the ComponentOperations method signatures.
func (m *ManifestOperations) errorResult(ctx context.Context, err error) (*controller.OperationResult, error) {
	return &controller.OperationResult{
		UpdatedStatus:  m.marshalStatus(ctx),
		Success:        false,
		OperationError: err,
	}, nil
}

// pendingResult creates an OperationResult for operations still in progress.
// Returns the result and nil error, matching the ComponentOperations method signatures.
func (m *ManifestOperations) pendingResult(ctx context.Context) (*controller.OperationResult, error) {
	return &controller.OperationResult{
		UpdatedStatus: m.marshalStatus(ctx),
		Success:       false,
	}, nil
}

// resourceRefToGVR converts a ResourceReference to GroupVersionResource using the REST mapper.
// Returns the GVR, REST mapping, and any error encountered.
func (m *ManifestOperations) resourceRefToGVR(ref ResourceReference) (schema.GroupVersionResource, *meta.RESTMapping, error) {
	// Parse GVK from reference
	gv, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil {
		return schema.GroupVersionResource{}, nil, fmt.Errorf("failed to parse apiVersion %s: %w", ref.APIVersion, err)
	}

	gvk := schema.GroupVersionKind{
		Group:   gv.Group,
		Version: gv.Version,
		Kind:    ref.Kind,
	}

	// Get GVR from GVK using REST mapper
	mapping, err := m.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return schema.GroupVersionResource{}, nil, fmt.Errorf("failed to get REST mapping for %s: %w", gvk.String(), err)
	}

	return mapping.Resource, mapping, nil
}

// getResourceInterface returns a properly scoped dynamic.ResourceInterface for the given reference.
// Handles both namespaced and cluster-scoped resources.
func (m *ManifestOperations) getResourceInterface(ref ResourceReference) (dynamic.ResourceInterface, error) {
	gvr, mapping, err := m.resourceRefToGVR(ref)
	if err != nil {
		return nil, err
	}

	// Determine if resource is namespaced
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if ref.Namespace == "" {
			return nil, fmt.Errorf("namespaced resource %s missing namespace in reference", ref.Kind)
		}
		return m.dynamicClient.Resource(gvr).Namespace(ref.Namespace), nil
	}

	// Cluster-scoped resource
	return m.dynamicClient.Resource(gvr), nil
}

// getResourceInterfaceForGVK returns a properly scoped dynamic.ResourceInterface for a given GVK and namespace.
// Handles both namespaced and cluster-scoped resources. Used during manifest application.
func (m *ManifestOperations) getResourceInterfaceForGVK(gvk schema.GroupVersionKind, namespace string) (dynamic.ResourceInterface, error) {
	// Convert GVK to ResourceReference and delegate to getResourceInterface
	ref := ResourceReference{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Namespace:  namespace,
		// Name is not needed for resource interface creation
	}
	return m.getResourceInterface(ref)
}
