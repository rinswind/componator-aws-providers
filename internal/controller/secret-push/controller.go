// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package secretpush

import (
	"time"

	"github.com/rinswind/componator/componentkit/controller"
	ctrl "sigs.k8s.io/controller-runtime"
)

//+kubebuilder:rbac:groups=deployment-orchestrator.io,resources=components,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=deployment-orchestrator.io,resources=components/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=deployment-orchestrator.io,resources=components/finalizers,verbs=update

// ComponentReconciler reconciles a Component object for secret-push handler using the generic
// controller base with AWS Secrets Manager-specific operations.
//
// This embeds the base controller directly, eliminating unnecessary delegation
// while maintaining protocol compliance.
type ComponentReconciler struct {
	*controller.ComponentReconciler
}

// NewComponentReconciler creates a new secret-push Component controller with the generic base
func NewComponentReconciler() *ComponentReconciler {
	factory := NewSecretPushOperationsFactory()

	config := controller.DefaultComponentReconcilerConfig(HandlerName)
	config.ErrorRequeue = 30 * time.Second       // Give time for AWS throttling/transient errors
	config.DefaultRequeue = 30 * time.Second     // Secrets Manager operations are generally fast
	config.StatusCheckRequeue = 15 * time.Second // Check secret status frequently

	return &ComponentReconciler{controller.NewComponentReconciler(factory, config)}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return r.ComponentReconciler.NewComponentController(mgr).
		Complete(r.ComponentReconciler)
}
