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

package base

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
	"github.com/rinswind/deployment-operator/handler/util"
)

// ComponentReconciler provides a generic Component controller implementation that handles
// all protocol state machine logic while delegating handler-specific operations to the
// injected ComponentOperations implementation.
//
// This enables code reuse across different deployment technologies (Helm, Terraform, etc.)
// while maintaining exact protocol compliance and error handling patterns.
type ComponentReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// Handler-specific dependencies injected during setup
	operations     ComponentOperations
	config         ComponentHandlerConfig
	claimValidator *util.ClaimingProtocolValidator
}

// NewComponentReconciler creates a new generic Component controller with the specified
// handler-specific operations and configuration.
func NewComponentReconciler(operations ComponentOperations, config ComponentHandlerConfig) *ComponentReconciler {
	claimValidator := util.NewClaimingProtocolValidator(config.HandlerName)

	return &ComponentReconciler{
		operations:     operations,
		config:         config,
		claimValidator: claimValidator,
	}
}

// SetupWithManager sets up the controller with the Manager using handler-specific configuration.
// Implements Resource Discovery Phase by only watching Components for this handler.
func (r *ComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Client = mgr.GetClient()
	r.Scheme = mgr.GetScheme()

	return ctrl.NewControllerManagedBy(mgr).
		For(&deploymentsv1alpha1.Component{}).
		WithEventFilter(ComponentHandlerPredicate(r.config.HandlerName)).
		Named(r.config.ControllerName).
		Complete(r)
}

// +kubebuilder:rbac:groups=deployments.deployment-orchestrator.io,resources=components,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=deployments.deployment-orchestrator.io,resources=components/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=deployments.deployment-orchestrator.io,resources=components/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// This implements the generic protocol state machine that works with any ComponentOperations
// implementation to provide consistent behavior across all deployment handlers.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *ComponentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	component := &deploymentsv1alpha1.Component{}
	if err := r.Get(ctx, req.NamespacedName, component); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling component", "component", component.Name, "handler", r.config.HandlerName)

	// Handle deletion
	if component.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, component)
	}

	// Handle creation/update
	return r.handleCreation(ctx, component)
}

// handleCreation implements the creation protocol for Components using the generic state machine
func (r *ComponentReconciler) handleCreation(
	ctx context.Context, component *deploymentsv1alpha1.Component) (ctrl.Result, error) {

	log := logf.FromContext(ctx).WithValues("component", component.Name, "phase", component.Status.Phase, "handler", r.config.HandlerName)
	handlerName := r.config.HandlerName

	// Check if this component is for us
	if err := r.claimValidator.CanClaim(component); err != nil {
		return ctrl.Result{}, fmt.Errorf("Component not for us: %w", err)
	}

	// 1. If nothing (logically in Pending) -> claim
	if !util.HasHandlerFinalizer(component, handlerName) {
		log.Info("Claiming component")

		util.AddHandlerFinalizer(component, handlerName)
		if err := r.Update(ctx, component); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to claim component: %w", err)
		}

		util.SetClaimedStatus(component, handlerName)
		return ctrl.Result{}, r.Status().Update(ctx, component)
	}

	// 2. If Claimed -> start deploying
	if util.IsClaimed(component) {
		log.Info("Starting deployment of component")

		// Set the status first so that if we fail we can safely retry without having done destructive ops
		util.SetDeployingStatus(component, handlerName)
		if err := r.Status().Update(ctx, component); err != nil {
			log.Error(err, "failed to update deploying status", "requeueAfter", r.config.ErrorRequeue)
			return ctrl.Result{RequeueAfter: r.config.ErrorRequeue}, err
		}

		err := r.operations.Deploy(ctx, component)
		if err != nil {
			log.Error(err, "failed to perform deployment")
			util.SetFailedStatus(component, handlerName, err.Error())
			return ctrl.Result{}, r.Status().Update(ctx, component)
		}

		// Start waiting for deployment to complete
		log.Info("Started deployment", "requeueAfter", r.config.StatusCheckRequeue)
		return ctrl.Result{RequeueAfter: r.config.StatusCheckRequeue}, nil
	}

	// 3. If Deploying -> check deployment progress
	if util.IsDeploying(component) {
		// Get elapsed time for timeout checking
		elapsed := util.GetPhaseElapsedTime(component)

		// Check deployment status - encapsulates all resource gathering logic including timeout
		ready, ioErr, deploymentErr := r.operations.CheckDeployment(ctx, component, elapsed)

		// Handle I/O errors with requeue
		if ioErr != nil {
			log.Error(ioErr, "failed to check deployment state", "requeueAfter", r.config.ErrorRequeue)
			return ctrl.Result{RequeueAfter: r.config.ErrorRequeue}, ioErr
		}

		// Handle deployment failures without requeue
		if deploymentErr != nil {
			log.Error(deploymentErr, "deployment failed")
			util.SetFailedStatus(component, handlerName, deploymentErr.Error())
			return ctrl.Result{}, r.Status().Update(ctx, component)
		}

		if ready {
			log.Info("Deployment succeeded")
			util.SetReadyStatus(component)
			return ctrl.Result{}, r.Status().Update(ctx, component)
		}

		// Resources not ready yet, requeue for another check
		log.Info("Deployment in progress", "requeueAfter", r.config.StatusCheckRequeue)
		return ctrl.Result{RequeueAfter: r.config.StatusCheckRequeue}, nil
	}

	// 4. If in terminal state -> check if dirty
	if util.IsReady(component) || util.IsFailed(component) {
		if !util.IsDirty(component) {
			// Nothing to do
			return ctrl.Result{}, nil
		}

		// Start upgrade and set back to Deploying
		log.Info("Component is dirty, starting upgrade")

		// Set the status before we start, so that if we fail to set it, we have not done destructive ops
		util.SetDeployingStatus(component, handlerName)
		if err := r.Status().Update(ctx, component); err != nil {
			log.Error(err, "failed to update deploying status", "requeueAfter", r.config.ErrorRequeue)
			return ctrl.Result{RequeueAfter: r.config.ErrorRequeue}, err
		}

		err := r.operations.Upgrade(ctx, component)
		if err != nil {
			log.Error(err, "failed to perform upgrade")
			util.SetFailedStatus(component, handlerName, err.Error())
			return ctrl.Result{}, r.Status().Update(ctx, component)
		}

		// Start waiting for upgrade to complete
		log.Info("Started upgrade", "requeueAfter", r.config.StatusCheckRequeue)
		return ctrl.Result{RequeueAfter: r.config.StatusCheckRequeue}, nil
	}

	return ctrl.Result{}, fmt.Errorf("Component in unexpected phase: %s", component.Status.Phase)
}

// handleDeletion implements the deletion protocol for Components being deleted using the generic state machine
func (r *ComponentReconciler) handleDeletion(
	ctx context.Context, component *deploymentsv1alpha1.Component) (ctrl.Result, error) {

	log := logf.FromContext(ctx).WithValues("component", component.Name, "phase", component.Status.Phase, "handler", r.config.HandlerName)
	handlerName := r.config.HandlerName

	// Check if we can proceed
	if err := r.claimValidator.CanDelete(component); err != nil {
		// Wait for composition coordination signal
		return ctrl.Result{RequeueAfter: r.config.DefaultRequeue}, fmt.Errorf("cannot proceed with deletion: %w", err)
	}

	// 1. If Termination has not started -> initiate deletion
	if !util.IsTerminating(component) {
		log.Info("Beginning component cleanup")

		util.SetTerminatingStatus(component, handlerName, "Initiating cleanup")
		if err := r.Status().Update(ctx, component); err != nil {
			log.Error(err, "failed to update terminating status")
			return ctrl.Result{RequeueAfter: r.config.ErrorRequeue}, nil
		}

		// Start async cleanup
		if err := r.operations.Delete(ctx, component); err != nil {
			log.Error(err, "failed to start cleanup")

			util.SetTerminatingStatus(component, handlerName, fmt.Sprintf("Cleanup initiation failed: %v", err))
			if statusErr := r.Status().Update(ctx, component); statusErr != nil {
				log.Error(statusErr, "failed to update failed status")
			}
		}

		log.Info("Started cleanup", "requeueAfter", r.config.StatusCheckRequeue)
		return ctrl.Result{RequeueAfter: r.config.StatusCheckRequeue}, nil
	}

	// 2. If Terminating -> check deletion progress
	elapsed := util.GetPhaseElapsedTime(component)

	// Check deletion status - encapsulates all resource checking logic including timeout
	deleted, ioErr, deletionErr := r.operations.CheckDeletion(ctx, component, elapsed)

	// Handle I/O errors with requeue (transient)
	if ioErr != nil {
		log.Error(ioErr, "failed to check deletion state", "requeueAfter", r.config.ErrorRequeue)
		return ctrl.Result{RequeueAfter: r.config.ErrorRequeue}, ioErr
	}

	// Handle deletion failures without requeue (permanent)
	if deletionErr != nil {
		log.Error(deletionErr, "deletion failed permanently")
		util.SetTerminatingStatus(component, handlerName, deletionErr.Error())
		return ctrl.Result{}, r.Status().Update(ctx, component)
		// Note: finalizer is NOT removed - component stays in Terminating state
	}

	if !deleted {
		log.Info("Deletion in progress", "requeueAfter", r.config.StatusCheckRequeue)
		return ctrl.Result{RequeueAfter: r.config.StatusCheckRequeue}, nil
	}

	// 3. Deletion complete -> remove finalizer
	log.Info("Component deletion completed")

	util.RemoveHandlerFinalizer(component, handlerName)
	if err := r.Update(ctx, component); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
	}

	log.Info("Component cleanup completed")
	return ctrl.Result{}, nil
}
