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

// deletion_protocol.go contains the deletion protocol implementation for helm components.
// This includes the deletion coordination logic using finalizers and cleanup procedures
// for helm releases when Components are being deleted.

package helm

import (
	"context"
	"fmt"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

// handleDeletion implements the deletion protocol for Components being deleted
func (r *ComponentReconciler) handleDeletion(ctx context.Context, component *deploymentsv1alpha1.Component) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Check if composition coordination finalizer is still present
	if controllerutil.ContainsFinalizer(component, deploymentsv1alpha1.ComponentCoordinationFinalizer) {
		// Wait for Composition controller to remove coordination finalizer
		log.Info("Waiting for composition coordination finalizer removal", "component", component.Name)
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	// Composition has signaled cleanup can proceed
	if r.isClaimedByUs(component) {
		log.Info("Beginning component cleanup", "component", component.Name)

		// Update status to Terminating
		component.Status.Phase = deploymentsv1alpha1.ComponentPhaseTerminating
		component.Status.Message = fmt.Sprintf("Cleaning up resources via %s handler", HandlerName)

		if err := r.Status().Update(ctx, component); err != nil {
			log.Error(err, "failed to update terminating status")
			// Don't return error - continue with cleanup
		}

		// TODO: Implement actual Helm cleanup in later tasks
		// For now, just simulate cleanup completion

		// Remove our finalizer to complete cleanup
		controllerutil.RemoveFinalizer(component, HandlerFinalizer)
		if err := r.Update(ctx, component); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
		}

		log.Info("Component cleanup completed", "component", component.Name)
	}

	return ctrl.Result{}, nil
}
