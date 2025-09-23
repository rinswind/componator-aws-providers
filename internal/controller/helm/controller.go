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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
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
	if component.Spec.Handler != "helm" {
		return ctrl.Result{}, nil
	}

	log.Info("Reconciling helm component", "component", component.Name)

	// Parse the Helm configuration from Component.Spec.Config
	config, err := r.parseHelmConfig(&component)
	if err != nil {
		log.Error(err, "failed to parse helm configuration")
		// TODO: Update component status to Failed with error message
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

	// TODO: Implement helm-specific reconciliation logic here
	// 1. Check if component is claimed by this controller
	// 2. Implement claiming protocol if not claimed
	// 3. Deploy/update helm chart based on component config
	// 4. Update component status

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&deploymentsv1alpha1.Component{}).
		Named("helm-component").
		Complete(r)
}
