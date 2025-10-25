// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package rds

import (
	"time"

	"github.com/rinswind/componator/componentkit/controller"
	ctrl "sigs.k8s.io/controller-runtime"
)

//+kubebuilder:rbac:groups=deployment-orchestrator.io,resources=components,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=deployment-orchestrator.io,resources=components/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=deployment-orchestrator.io,resources=components/finalizers,verbs=update

// ComponentReconciler reconciles a Component object for rds handler using the generic
// controller base with RDS-specific operations.
//
// This embeds the base controller directly, eliminating unnecessary delegation
// while maintaining protocol compliance.
type ComponentReconciler struct {
	*controller.ComponentReconciler
}

// NewComponentReconciler creates a new RDS Component controller with the generic base
func NewComponentReconciler() *ComponentReconciler {
	factory := NewRdsOperationsFactory()

	config := controller.DefaultComponentReconcilerConfig("rds")
	config.ErrorRequeue = 15 * time.Second
	config.DefaultRequeue = 30 * time.Second
	config.StatusCheckRequeue = 30 * time.Second

	return &ComponentReconciler{controller.NewComponentReconciler(factory, config)}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return r.ComponentReconciler.NewComponentController(mgr).
		Complete(r.ComponentReconciler)
}
