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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

const (
	// HandlerName is the identifier for this helm handler
	HandlerName = "helm"

	// HandlerFinalizer is the finalizer used for claiming Components
	HandlerFinalizer = "helm.deployment-orchestrator.io/lifecycle"

	// CoordinationFinalizer is the finalizer used for dual finalizer coordination pattern on Components
	CoordinationFinalizer = "composition.deployment-orchestrator.io/coordination"
)

// HelmConfig represents the configuration structure for Helm components
// that gets unmarshaled from Component.Spec.Config
type HelmConfig struct {
	// Repository specifies the Helm chart repository configuration
	Repository HelmRepository `json:"repository"`

	// Chart specifies the chart name and version to deploy
	Chart HelmChart `json:"chart"`

	// Values contains key-value pairs for chart values override
	// +optional
	Values map[string]string `json:"values,omitempty"`

	// Namespace specifies the target namespace for chart deployment
	// If not specified, uses the Component's namespace
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// HelmRepository represents Helm chart repository configuration
type HelmRepository struct {
	// URL is the chart repository URL
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=^https?://.*
	URL string `json:"url"`

	// Name is the repository name for local reference
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// HelmChart represents chart identification and version specification
type HelmChart struct {
	// Name is the chart name within the repository
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Version specifies the chart version to install
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`
}

// ComponentReconciler reconciles a Component object for helm handler
type ComponentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// parseHelmConfig unmarshals Component.Spec.Config into HelmConfig struct
func (r *ComponentReconciler) parseHelmConfig(component *deploymentsv1alpha1.Component) (*HelmConfig, error) {
	if component.Spec.Config == nil {
		return nil, fmt.Errorf("config is required for helm components")
	}

	var config HelmConfig
	if err := json.Unmarshal(component.Spec.Config.Raw, &config); err != nil {
		return nil, fmt.Errorf("failed to parse helm config: %w", err)
	}

	// Validate required fields
	if config.Repository.URL == "" {
		return nil, fmt.Errorf("repository.url is required")
	}
	if config.Repository.Name == "" {
		return nil, fmt.Errorf("repository.name is required")
	}
	if config.Chart.Name == "" {
		return nil, fmt.Errorf("chart.name is required")
	}
	if config.Chart.Version == "" {
		return nil, fmt.Errorf("chart.version is required")
	}

	return &config, nil
}

// generateReleaseName creates a deterministic release name from Component metadata
func (r *ComponentReconciler) generateReleaseName(component *deploymentsv1alpha1.Component) string {
	// Use component name and namespace to ensure uniqueness
	return fmt.Sprintf("%s-%s", component.Namespace, component.Name)
}

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

// handleDeletion implements the deletion protocol for Components being deleted
func (r *ComponentReconciler) handleDeletion(ctx context.Context, component *deploymentsv1alpha1.Component) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Check if composition coordination finalizer is still present
	if controllerutil.ContainsFinalizer(component, CoordinationFinalizer) {
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
