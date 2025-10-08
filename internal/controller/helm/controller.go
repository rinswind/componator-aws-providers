// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"github.com/rinswind/deployment-operator/componentkit/controller"
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

// NewComponentReconciler creates a new Helm Component controller with the generic base using factory pattern
func NewComponentReconciler() *ComponentReconciler {
	operationsFactory := &HelmOperationsFactory{}
	config := controller.DefaultComponentReconcilerConfig("helm")

	return &ComponentReconciler{
		ComponentReconciler: controller.NewComponentReconciler(operationsFactory, config),
	}
}
