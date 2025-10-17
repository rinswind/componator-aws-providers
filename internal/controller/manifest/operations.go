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
