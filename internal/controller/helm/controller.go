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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

const (
	// HandlerName is the identifier for this helm handler
	HandlerName = "helm"

	// HandlerFinalizer is the finalizer used for claiming Components
	HandlerFinalizer = "helm.deployment-orchestrator.io/lifecycle"
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

	// Fetch the Component instance
	var component deploymentsv1alpha1.Component
	if err := r.Get(ctx, req.NamespacedName, &component); err != nil {
		log.Error(err, "unable to fetch Component")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Only process components for this handler
	if component.Spec.Handler != HandlerName {
		return ctrl.Result{}, nil
	}

	log.Info("Reconciling helm component", "component", component.Name)

	// Handle deletion if Component is being deleted
	if component.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, &component)
	}

	// Implement claiming protocol
	if result, err := r.claimingProtocol(ctx, &component); err != nil {
		return result, err
	}

	// At this point, component should be claimed by us
	if !r.isClaimedByUs(&component) {
		// This shouldn't happen, but if it does, skip processing
		log.V(1).Info("Component not claimed by us, skipping", "component", component.Name)
		return ctrl.Result{}, nil
	}

	// Parse the Helm configuration from Component.Spec.Config
	config, err := r.parseHelmConfig(&component)
	if err != nil {
		log.Error(err, "failed to parse helm configuration")

		// Update component status to Failed with error message
		component.Status.Phase = deploymentsv1alpha1.ComponentPhaseFailed
		component.Status.Message = fmt.Sprintf("Configuration error: %v", err)

		if statusErr := r.Status().Update(ctx, &component); statusErr != nil {
			log.Error(statusErr, "failed to update failed status")
		}

		return ctrl.Result{}, err
	}

	// Generate deterministic release name
	releaseName := r.generateReleaseName(&component)

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

	// TODO: Implement helm-specific deployment logic in later tasks
	// For now, just log that the component is claimed and ready for deployment

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&deploymentsv1alpha1.Component{}).
		Named("helm-component").
		Complete(r)
}
