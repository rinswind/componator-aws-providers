/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// claiming_protocol.go contains the claiming protocol implementation for helm components.
// This includes atomic component claiming logic using handler-specific finalizers
// and methods to check component claim status.

package helm

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

// hasAnyHandlerFinalizer checks if the Component has any handler-specific finalizer
func (r *ComponentReconciler) hasAnyHandlerFinalizer(component *deploymentsv1alpha1.Component) bool {
	for _, finalizer := range component.Finalizers {
		// Check for handler lifecycle finalizers (excluding coordination finalizer)
		if strings.HasSuffix(finalizer, ".deployment-orchestrator.io/lifecycle") &&
			!strings.HasPrefix(finalizer, "composition.") {
			return true
		}
	}
	return false
}

// claimComponent atomically claims a Component by adding the handler finalizer
func (r *ComponentReconciler) claimComponent(ctx context.Context, component *deploymentsv1alpha1.Component) error {
	log := logf.FromContext(ctx)

	// Add our finalizer for atomic claiming
	controllerutil.AddFinalizer(component, HandlerFinalizer)

	// Update the Component resource (this is the atomic operation)
	if err := r.Update(ctx, component); err != nil {
		return fmt.Errorf("failed to claim component: %w", err)
	}

	// Update status to reflect claiming
	component.Status.Phase = deploymentsv1alpha1.ComponentPhaseClaimed
	component.Status.ClaimedBy = HandlerName
	now := metav1.Now()
	component.Status.ClaimedAt = &now
	component.Status.Message = fmt.Sprintf("Claimed by %s handler", HandlerName)

	// Update status subresource
	if err := r.Status().Update(ctx, component); err != nil {
		log.Error(err, "failed to update claimed status", "component", component.Name)
		return fmt.Errorf("failed to update claimed status: %w", err)
	}

	log.Info("Successfully claimed component", "component", component.Name, "finalizer", HandlerFinalizer)
	return nil
}

// isClaimedByUs checks if this Component is already claimed by this handler
func (r *ComponentReconciler) isClaimedByUs(component *deploymentsv1alpha1.Component) bool {
	return controllerutil.ContainsFinalizer(component, HandlerFinalizer)
}

// claimingProtocol implements the claiming protocol logic
func (r *ComponentReconciler) claimingProtocol(ctx context.Context, component *deploymentsv1alpha1.Component) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Check if already claimed by us
	if r.isClaimedByUs(component) {
		log.V(1).Info("Component already claimed by this handler", "component", component.Name)
		return ctrl.Result{}, nil
	}

	// Check if claimed by different handler
	if r.hasAnyHandlerFinalizer(component) {
		log.V(1).Info("Component claimed by different handler, skipping", "component", component.Name)
		return ctrl.Result{}, nil
	}

	// Component is available for claiming
	log.Info("Claiming available component", "component", component.Name)
	if err := r.claimComponent(ctx, component); err != nil {
		return ctrl.Result{}, err
	}

	// Successfully claimed - continue processing in this reconciliation
	return ctrl.Result{}, nil
}
