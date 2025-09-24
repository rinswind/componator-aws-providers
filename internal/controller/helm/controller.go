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

package helm

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	controllerutils "github.com/rinswind/deployment-handlers/internal/controller"
	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
	"github.com/rinswind/deployment-operator/handler/util"
)

const (
	// HandlerName is the identifier for this helm handler
	HandlerName = "helm"
)

// ComponentReconciler reconciles a Component object for helm handler
type ComponentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=deployments.deployment-orchestrator.io,resources=components,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=deployments.deployment-orchestrator.io,resources=components/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=deployments.deployment-orchestrator.io,resources=components/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For helm components, this means deploying and managing Helm charts based on Component specs.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *ComponentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 1. Fetch Component
	component := &deploymentsv1alpha1.Component{}
	if err := r.Get(ctx, req.NamespacedName, component); err != nil {
		log.Error(err, "unable to fetch Component")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2. Create protocol validator
	validator := util.NewClaimingProtocolValidator(HandlerName)

	// 3. Filter by handler name
	if validator.ShouldIgnore(component) {
		return ctrl.Result{}, nil
	}

	log.Info("Reconciling helm component", "component", component.Name)

	// 4. Handle deletion if DeletionTimestamp set
	if util.IsTerminating(component) {
		return r.handleDeletion(ctx, component, validator)
	}

	// 5. Implement claiming protocol and creation/deployment logic
	return r.handleCreation(ctx, component, validator)
}

// handleCreation implements the creation protocol for Components
func (r *ComponentReconciler) handleCreation(ctx context.Context, component *deploymentsv1alpha1.Component, validator *util.ClaimingProtocolValidator) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Check if we can claim
	if err := validator.CanClaim(component); err != nil {
		return ctrl.Result{}, err
	}

	// Claim if not already claimed by us
	if !util.HasHandlerFinalizer(component, HandlerName) {
		log.Info("Claiming available component", "component", component.Name)
		util.AddHandlerFinalizer(component, HandlerName)
		if err := r.Update(ctx, component); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to claim component: %w", err)
		}
		util.SetClaimedStatus(component, HandlerName)
		return ctrl.Result{}, r.Status().Update(ctx, component)
	}

	// Parse the Helm configuration from Component.Spec.Config
	config, err := r.parseHelmConfig(component)
	if err != nil {
		log.Error(err, "failed to parse helm configuration")
		util.SetFailedStatus(component, HandlerName, fmt.Sprintf("Configuration error: %v", err))
		return ctrl.Result{}, r.Status().Update(ctx, component)
	}

	// Set deploying status if not already set
	if !util.IsPhase(component, deploymentsv1alpha1.ComponentPhaseDeploying) {
		util.SetDeployingStatus(component, HandlerName)
		if err := r.Status().Update(ctx, component); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Generate deterministic release name
	releaseName := r.generateReleaseName(component)

	// Determine target namespace (use config.Namespace if specified, otherwise component namespace)
	targetNamespace := component.Namespace
	if config.Namespace != "" {
		targetNamespace = config.Namespace
	}

	log.Info("Parsed helm configuration",
		"repository", config.Repository.URL,
		"chart", config.Chart.Name,
		"version", config.Chart.Version,
		"releaseName", releaseName,
		"targetNamespace", targetNamespace,
		"valuesCount", len(config.Values))

	// TODO: Implement actual Helm deployment logic in later tasks
	// For now, just mark as ready after claiming and configuration parsing
	if !util.IsReady(component) {
		util.SetReadyStatus(component)
		return ctrl.Result{}, r.Status().Update(ctx, component)
	}

	return ctrl.Result{}, nil
}

// handleDeletion implements the deletion protocol for Components being deleted
func (r *ComponentReconciler) handleDeletion(ctx context.Context, component *deploymentsv1alpha1.Component, validator *util.ClaimingProtocolValidator) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Check if we can delete
	if err := validator.CanDelete(component); err != nil {
		// Wait for composition coordination signal
		log.Info("Waiting for composition coordination finalizer removal", "component", component.Name)
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	// Set terminating status if not already set
	if !util.IsPhase(component, deploymentsv1alpha1.ComponentPhaseTerminating) {
		log.Info("Beginning component cleanup", "component", component.Name)
		util.SetTerminatingStatus(component, HandlerName)
		if err := r.Status().Update(ctx, component); err != nil {
			log.Error(err, "failed to update terminating status")
			// Don't return error - continue with cleanup
		}
	}

	// TODO: Implement actual Helm cleanup in later tasks
	// For now, just simulate cleanup completion

	// Remove our finalizer to complete cleanup
	util.RemoveHandlerFinalizer(component, HandlerName)
	if err := r.Update(ctx, component); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
	}

	log.Info("Component cleanup completed", "component", component.Name)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
// Implements Resource Discovery Phase by only watching Components with handler "helm"
func (r *ComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&deploymentsv1alpha1.Component{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 5,
		}).
		WithEventFilter(controllerutils.ComponentHandlerPredicate(HandlerName)).
		Named("helm-component").
		Complete(r)
}
