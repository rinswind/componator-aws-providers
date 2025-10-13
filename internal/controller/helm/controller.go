// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"time"

	"github.com/rinswind/deployment-operator-handlers/internal/controller/helm/sources/composite"
	httpsource "github.com/rinswind/deployment-operator-handlers/internal/controller/helm/sources/http"
	ocisource "github.com/rinswind/deployment-operator-handlers/internal/controller/helm/sources/oci"
	"github.com/rinswind/deployment-operator/componentkit/controller"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Configuration values for Helm chart source
	helmBasePath         = "/helm"
	indexCacheSize       = 10
	indexCacheTTL        = 1 * time.Hour
	indexRefreshInterval = 5 * time.Minute
)

//+kubebuilder:rbac:groups=deployment-orchestrator.io,resources=components,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=deployment-orchestrator.io,resources=components/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=deployment-orchestrator.io,resources=components/finalizers,verbs=update
//+kubebuilder:rbac:groups="*",resources="*",verbs="*"

// ComponentReconciler reconciles a Component object for helm handler using the generic
// controller base with Helm-specific operations factory.
//
// This embeds the base controller directly, eliminating unnecessary delegation
// while maintaining protocol compliance and using the factory pattern for
// efficient configuration parsing.
type ComponentReconciler struct {
	*controller.ComponentReconciler
}

// NewComponentReconciler creates a new Helm Component controller with the generic base using factory pattern.
// Returns error if initialization fails (e.g., unable to create required directories).
// Note: k8sClient parameter added for OCI registry credential resolution.
func NewComponentReconciler(k8sClient client.Client) (*ComponentReconciler, error) {
	// Create HTTP caching repository singleton (shared across all HTTP sources)
	httpRepo, err := httpsource.NewCachingRepository(
		helmBasePath,
		indexCacheSize,
		indexCacheTTL,
		indexRefreshInterval,
	)
	if err != nil {
		return nil, err
	}

	// Create factory instances (stateless singletons)
	// Both HTTP and OCI sources share the same repository cache directory
	httpFactory := httpsource.NewFactory(httpRepo)
	ociFactory := ocisource.NewFactory(k8sClient, helmBasePath)

	// Create and populate factory registry
	registry := composite.NewFactory()
	registry.Register(httpFactory)
	registry.Register(ociFactory)

	// Create operations factory with factory registry
	operationsFactory := NewHelmOperationsFactory(registry)

	config := controller.DefaultComponentReconcilerConfig("helm")

	return &ComponentReconciler{controller.NewComponentReconciler(operationsFactory, config)}, nil
}
