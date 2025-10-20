// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package iampolicy

import (
	"time"

	"github.com/rinswind/deployment-operator/componentkit/controller"
	ctrl "sigs.k8s.io/controller-runtime"
)

//+kubebuilder:rbac:groups=deployment-orchestrator.io,resources=components,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=deployment-orchestrator.io,resources=components/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=deployment-orchestrator.io,resources=components/finalizers,verbs=update

// ComponentReconciler reconciles a Component object for iam-policy handler using the generic
// controller base with IAM-specific operations.
//
// This embeds the base controller directly, eliminating unnecessary delegation
// while maintaining protocol compliance.
type ComponentReconciler struct {
	*controller.ComponentReconciler
}

// NewComponentReconciler creates a new IAM policy Component controller with the generic base
func NewComponentReconciler() *ComponentReconciler {
	factory := NewIamPolicyOperationsFactory()

	config := controller.DefaultComponentReconcilerConfig(HandlerName)
	config.ErrorRequeue = 30 * time.Second       // Give more time for AWS throttling errors
	config.DefaultRequeue = 15 * time.Second     // IAM operations are generally fast
	config.StatusCheckRequeue = 10 * time.Second // Check policy status frequently

	return &ComponentReconciler{controller.NewComponentReconciler(factory, config)}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return r.ComponentReconciler.NewComponentController(mgr).
		Complete(r.ComponentReconciler)
}
