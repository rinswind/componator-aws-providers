// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rinswind/deployment-operator/componentkit/controller"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

// getResourceInterface returns a properly scoped dynamic.ResourceInterface for the given reference.
// Handles both namespaced and cluster-scoped resources.
func (m *ManifestOperations) getResourceInterface(ref ResourceReference) (dynamic.ResourceInterface, error) {
	// Parse GVK from reference
	gv, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to parse apiVersion %s: %w", ref.APIVersion, err)
	}

	gvk := schema.GroupVersionKind{
		Group:   gv.Group,
		Version: gv.Version,
		Kind:    ref.Kind,
	}

	// Get GVR from GVK using REST mapper
	mapping, err := m.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to get REST mapping for %s: %w", gvk.String(), err)
	}

	return m.resourceInterfaceFromMapping(mapping.Resource, mapping, ref.Namespace, ref.Kind)
}

// getResourceInterfaceForGVK returns a properly scoped dynamic.ResourceInterface for a given GVK and namespace.
// Handles both namespaced and cluster-scoped resources. Used during manifest application.
func (m *ManifestOperations) getResourceInterfaceForGVK(
	gvk schema.GroupVersionKind, namespace string) (dynamic.ResourceInterface, error) {

	mapping, err := m.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to get REST mapping for %s: %w", gvk.String(), err)
	}

	return m.resourceInterfaceFromMapping(mapping.Resource, mapping, namespace, gvk.String())
}

// resourceInterfaceFromMapping creates a properly scoped ResourceInterface from GVR, mapping, and namespace.
// Handles both namespaced and cluster-scoped resources.
func (m *ManifestOperations) resourceInterfaceFromMapping(
	gvr schema.GroupVersionResource,
	mapping *meta.RESTMapping,
	namespace string,
	resourceDesc string) (dynamic.ResourceInterface, error) {

	// Determine if resource is namespaced
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if namespace == "" {
			return nil, fmt.Errorf("namespaced resource %s missing namespace", resourceDesc)
		}
		return m.dynamicClient.Resource(gvr).Namespace(namespace), nil
	}

	// Cluster-scoped resource
	return m.dynamicClient.Resource(gvr), nil
}

// actionSuccessResult creates an ActionResult for successful Deploy/Delete operations
func (m *ManifestOperations) actionSuccessResult() (*controller.ActionResult, error) {
	updatedStatus, _ := json.Marshal(m.status)
	return &controller.ActionResult{
		UpdatedStatus: updatedStatus,
	}, nil
}

// actionFailureResult creates an ActionResult for permanent failures in Deploy/Delete operations
func (m *ManifestOperations) actionFailureResult(err error) (*controller.ActionResult, error) {
	updatedStatus, _ := json.Marshal(m.status)
	return &controller.ActionResult{
		UpdatedStatus:    updatedStatus,
		PermanentFailure: err,
	}, nil
}

// checkCompleteResult creates a CheckResult for completed check operations
func (m *ManifestOperations) checkCompleteResult() (*controller.CheckResult, error) {
	updatedStatus, _ := json.Marshal(m.status)
	return &controller.CheckResult{
		UpdatedStatus: updatedStatus,
		Complete:      true,
	}, nil
}

// checkInProgressResult creates a CheckResult for check operations still in progress
func (m *ManifestOperations) checkInProgressResult() (*controller.CheckResult, error) {
	updatedStatus, _ := json.Marshal(m.status)
	return &controller.CheckResult{
		UpdatedStatus: updatedStatus,
		Complete:      false,
	}, nil
}

// checkFailureResult creates a CheckResult for permanent failures in check operations
func (m *ManifestOperations) checkFailureResult(err error) (*controller.CheckResult, error) {
	updatedStatus, _ := json.Marshal(m.status)
	return &controller.CheckResult{
		UpdatedStatus:    updatedStatus,
		PermanentFailure: err,
	}, nil
}

// newActionResultForError creates a standardized error response for manifest action operations.
// Uses Kubernetes apierrors classification to distinguish retryable errors (network, timeouts, rate limiting)
// from permanent errors (validation, auth). Returns ActionResult with error for retryable issues.
func (m *ManifestOperations) newActionResultForError(err error) (*controller.ActionResult, error) {
	updatedStatus, _ := json.Marshal(m.status)

	// Check if this error should be retried
	if isRetryable(err) {
		return &controller.ActionResult{UpdatedStatus: updatedStatus}, err
	}

	// Permanent error
	return m.actionFailureResult(err)
}

// newCheckResultForError creates a standardized error response for manifest check operations.
// Uses Kubernetes apierrors classification to distinguish retryable errors (network, timeouts, rate limiting)
// from permanent errors (validation, auth). Returns CheckResult with error for retryable issues.
func (m *ManifestOperations) newCheckResultForError(err error) (*controller.CheckResult, error) {
	updatedStatus, _ := json.Marshal(m.status)

	// Check if this error should be retried
	if isRetryable(err) {
		return &controller.CheckResult{UpdatedStatus: updatedStatus}, err
	}

	// Permanent error
	return m.checkFailureResult(err)
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
