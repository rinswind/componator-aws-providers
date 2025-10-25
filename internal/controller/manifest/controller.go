// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"github.com/rinswind/componator/componentkit/controller"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	// HandlerName is the identifier for this manifest handler
	HandlerName = "manifest"
)

//+kubebuilder:rbac:groups=deployment-orchestrator.io,resources=components,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=deployment-orchestrator.io,resources=components/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=deployment-orchestrator.io,resources=components/finalizers,verbs=update
//+kubebuilder:rbac:groups="*",resources="*",verbs="*"

// ComponentReconciler reconciles a Component object for manifest handler using the generic
// controller base with Manifest-specific operations factory.
//
// This embeds the base controller directly, eliminating unnecessary delegation
// while maintaining protocol compliance and using the factory pattern for
// efficient configuration parsing.
type ComponentReconciler struct {
	*controller.ComponentReconciler
}

// NewComponentReconciler creates a new Manifest Component controller with the generic base using factory pattern.
// The dynamic client and REST mapper are extracted from the manager for applying arbitrary Kubernetes resources.
func NewComponentReconciler(mgr ctrl.Manager) (*ComponentReconciler, error) {
	// Create dynamic client from manager config for arbitrary resource support
	dynamicClient, err := dynamic.NewForConfig(mgr.GetConfig())
	if err != nil {
		return nil, err
	}

	// Get REST mapper from manager for GVK to GVR conversion
	restMapper := mgr.GetRESTMapper()

	operationsFactory := NewManifestOperationsFactory(dynamicClient, restMapper)
	config := controller.DefaultComponentReconcilerConfig(HandlerName)

	return &ComponentReconciler{controller.NewComponentReconciler(operationsFactory, config)}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return r.ComponentReconciler.NewComponentController(mgr).
		Complete(r.ComponentReconciler)
}
