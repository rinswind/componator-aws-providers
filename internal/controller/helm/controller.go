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
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	controllerutils "github.com/rinswind/deployment-handlers/internal/controller"
	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
	"github.com/rinswind/deployment-operator/handler/util"
)

const (
	// HandlerName is the identifier for this helm handler
	HandlerName = "helm"

	ControllerName = "helm-component"
)

// ComponentReconciler reconciles a Component object for helm handler
type ComponentReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	claimValidator *util.ClaimingProtocolValidator
	requeuePeriod  time.Duration

	// Default timeout configurations
	defaultDeploymentTimeout time.Duration
	defaultDeletionTimeout   time.Duration
}

// SetupWithManager sets up the controller with the Manager.
// Implements Resource Discovery Phase by only watching Components with handler "helm"
func (r *ComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Client = mgr.GetClient()
	r.Scheme = mgr.GetScheme()
	r.claimValidator = util.NewClaimingProtocolValidator(HandlerName)

	// Configure timeouts from environment variables with defaults
	r.requeuePeriod = parseTimeoutEnv("HELM_REQUEUE_PERIOD", 10*time.Second)
	r.defaultDeploymentTimeout = parseTimeoutEnv("HELM_DEPLOYMENT_TIMEOUT", 15*time.Minute)
	r.defaultDeletionTimeout = parseTimeoutEnv("HELM_DELETION_TIMEOUT", 30*time.Minute)

	return ctrl.NewControllerManagedBy(mgr).
		For(&deploymentsv1alpha1.Component{}).
		// WithOptions(controller.Options{
		// 	MaxConcurrentReconciles: 1,
		// }).
		WithEventFilter(controllerutils.ComponentHandlerPredicate(HandlerName)).
		Named(ControllerName).
		Complete(r)
}

// parseTimeoutEnv parses a timeout duration from an environment variable
// Returns the default value if the environment variable is not set or invalid
func parseTimeoutEnv(envVar string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(envVar); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
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

	component := &deploymentsv1alpha1.Component{}
	if err := r.Get(ctx, req.NamespacedName, component); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling component", "component", component.Name)

	// Handle deletion
	if component.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, component)
	}

	// Handle creation/update
	return r.handleCreation(ctx, component)
}

// handleCreation implements the creation protocol for Components
func (r *ComponentReconciler) handleCreation(
	ctx context.Context, component *deploymentsv1alpha1.Component) (ctrl.Result, error) {

	log := logf.FromContext(ctx).WithValues("component", component.Name, "phase", component.Status.Phase)

	// Check if this component is for us
	if err := r.claimValidator.CanClaim(component); err != nil {
		return ctrl.Result{}, fmt.Errorf("Component not for us: %w", err)
	}

	// 1. If nothing (logically in Pending) -> claim
	if !util.HasHandlerFinalizer(component, HandlerName) {
		log.Info("Claiming component")

		util.AddHandlerFinalizer(component, HandlerName)
		if err := r.Update(ctx, component); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to claim component: %w", err)
		}

		util.SetClaimedStatus(component, HandlerName)
		return ctrl.Result{}, r.Status().Update(ctx, component)
	}

	// 2. If Claimed -> start deploying
	if util.IsClaimed(component) {
		log.Info("Starting deployment of component")

		err := startHelmReleaseDeployment(ctx, component)
		if err != nil {
			log.Error(err, "failed to perform helm deployment")
			util.SetFailedStatus(component, HandlerName, err.Error())
			return ctrl.Result{}, r.Status().Update(ctx, component)
		}

		util.SetDeployingStatus(component, HandlerName)
		if err := r.Status().Update(ctx, component); err != nil {
			log.Error(err, "failed to update deploying status")
			return ctrl.Result{}, err
		}

		// Start waiting for deployment to complete
		log.Info("Started deployment, checking again in 10 seconds")
		return ctrl.Result{RequeueAfter: r.requeuePeriod}, nil
	}

	// 3. If Deploying -> check deployment progress
	if util.IsDeploying(component) {
		rel, err := getHelmRelease(ctx, component)
		if err != nil {
			log.Error(err, "failed to check helm release readiness")
			return ctrl.Result{RequeueAfter: 10 * time.Second}, r.Status().Update(ctx, component)
		}

		// Build ResourceList from release manifest for non-blocking status checking
		resourceList, err := gatherHelmReleaseResources(ctx, rel)
		if err != nil {
			log.Error(err, "failed to build resource list from release")
			return ctrl.Result{RequeueAfter: r.requeuePeriod}, nil
		}

		// Use non-blocking readiness check
		ready, err := checkHelmReleaseReady(ctx, rel.Namespace, resourceList)
		if err != nil {
			log.Error(err, "deployment failed")
			util.SetFailedStatus(component, HandlerName, err.Error())
			return ctrl.Result{}, r.Status().Update(ctx, component)
		}

		if ready {
			log.Info("Deployment succeeded")
			util.SetReadyStatus(component)
			return ctrl.Result{}, r.Status().Update(ctx, component)
		}

		// Resources not ready yet, requeue for another check
		log.Info("Helm release resources not ready yet, checking again in 10 seconds")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// 4. If in terminal state -> check if dirty
	if util.IsReady(component) || util.IsFailed(component) {
		if !util.IsDirty(component) {
			// Nothing to do
			return ctrl.Result{}, nil
		}

		// Start upgrade and set back to Deploying
		log.Info("Component is dirty, starting helm upgrade")

		err := startHelmReleaseUpgrade(ctx, component)
		if err != nil {
			log.Error(err, "failed to perform helm upgrade")
			util.SetFailedStatus(component, HandlerName, err.Error())
			return ctrl.Result{}, r.Status().Update(ctx, component)
		}

		util.SetDeployingStatus(component, HandlerName)
		if err := r.Status().Update(ctx, component); err != nil {
			log.Error(err, "failed to update deploying status")
			return ctrl.Result{}, err
		}

		// Start waiting for upgrade to complete
		log.Info("Started upgrade, checking again in 10 seconds")
		return ctrl.Result{RequeueAfter: r.requeuePeriod}, nil
	}

	return ctrl.Result{}, fmt.Errorf("Component in unexpected phase: %s", component.Status.Phase)

}

// handleDeletion implements the deletion protocol for Components being deleted
func (r *ComponentReconciler) handleDeletion(
	ctx context.Context, component *deploymentsv1alpha1.Component) (ctrl.Result, error) {

	log := logf.FromContext(ctx).WithValues("component", component.Name, "phase", component.Status.Phase)

	// Check if we can proceed
	if err := r.claimValidator.CanDelete(component); err != nil {
		// Wait for composition coordination signal
		return ctrl.Result{RequeueAfter: r.requeuePeriod}, fmt.Errorf("cannot proceed with deletion: %w", err)
	}

	// 1. If Termination has not started -> initiate deletion
	if !util.IsTerminating(component) {
		log.Info("Beginning component cleanup")

		util.SetTerminatingStatus(component, HandlerName, "Initiating cleanup")
		if err := r.Status().Update(ctx, component); err != nil {
			log.Error(err, "failed to update terminating status")
			return ctrl.Result{RequeueAfter: r.requeuePeriod}, nil
		}

		// Start async helm cleanup
		if err := startHelmReleaseDeletion(ctx, component); err != nil {
			log.Error(err, "failed to start helm cleanup")

			util.SetTerminatingStatus(component, HandlerName, fmt.Sprintf("Cleanup initiation failed: %v", err))
			if statusErr := r.Status().Update(ctx, component); statusErr != nil {
				log.Error(statusErr, "failed to update failed status")
			}
		}

		log.Info("Started cleanup, checking again in 10 seconds")
		return ctrl.Result{RequeueAfter: r.requeuePeriod}, nil
	}

	// 2. If Terminating -> check deletion progress
	deleted, err := checkHelmReleaseDeleted(ctx, component)
	if err != nil {
		log.Error(err, "deletion failed")
		util.SetTerminatingStatus(component, HandlerName, err.Error())
		return ctrl.Result{}, r.Status().Update(ctx, component)
	}

	if !deleted {
		log.Info("Deletion in progress, checking again in 10 seconds")
		return ctrl.Result{RequeueAfter: r.requeuePeriod}, nil
	}

	// 3. Deletion complete -> remove finalizer
	log.Info("Helm release deletion completed")

	util.RemoveHandlerFinalizer(component, HandlerName)
	if err := r.Update(ctx, component); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
	}

	log.Info("Component cleanup completed")
	return ctrl.Result{}, nil
}
