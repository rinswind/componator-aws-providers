// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package configreader

import (
	"context"
	"encoding/json"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/core/v1alpha1"
	"github.com/rinswind/deployment-operator/componentkit/controller"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

//+kubebuilder:rbac:groups=deployment-orchestrator.io,resources=components,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=deployment-orchestrator.io,resources=components/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=deployment-orchestrator.io,resources=components/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch

// ComponentReconciler reconciles a Component object for config-reader handler using the generic
// controller base with config-reader-specific operations factory.
type ComponentReconciler struct {
	*controller.ComponentReconciler
	client client.Client
}

// NewComponentReconciler creates a new config-reader Component controller.
func NewComponentReconciler(mgr ctrl.Manager) *ComponentReconciler {
	// Create operations factory with APIReader for non-cached ConfigMap access
	operationsFactory := NewConfigReaderOperationsFactory(mgr.GetAPIReader())

	config := controller.DefaultComponentReconcilerConfig("config-reader")

	return &ComponentReconciler{
		ComponentReconciler: controller.NewComponentReconciler(operationsFactory, config),
		client:              mgr.GetClient(),
	}
}

// SetupWithManager sets up the controller with the Manager and adds ConfigMap watch.
func (r *ComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return r.ComponentReconciler.NewDefaultController(mgr).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.mapConfigMapToComponents),
		).
		Complete(r.ComponentReconciler)
}

// mapConfigMapToComponents finds all config-reader Components that reference the changed ConfigMap.
func (r *ComponentReconciler) mapConfigMapToComponents(ctx context.Context, obj client.Object) []reconcile.Request {
	log := logf.FromContext(ctx)

	configMap, ok := obj.(*corev1.ConfigMap)
	if !ok {
		log.Error(nil, "Expected ConfigMap in mapper", "type", obj.GetObjectKind())
		return nil
	}

	log.V(1).Info("ConfigMap changed, finding affected Components",
		"configMap", configMap.Name,
		"namespace", configMap.Namespace)

	// List all Components with handler="config-reader"
	var componentList deploymentsv1alpha1.ComponentList
	if err := r.client.List(ctx, &componentList, client.MatchingFields{
		"spec.handler": "config-reader",
	}); err != nil {
		log.Error(err, "Failed to list config-reader Components")
		return nil
	}

	var requests []reconcile.Request

	// Check each Component's config to see if it references this ConfigMap
	for _, component := range componentList.Items {
		if r.componentReferencesConfigMap(&component, configMap.Namespace, configMap.Name) {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&component),
			})
			log.V(1).Info("Found Component referencing ConfigMap",
				"component", component.Name,
				"componentNamespace", component.Namespace,
				"configMap", configMap.Name)
		}
	}

	log.Info("Mapped ConfigMap to Components",
		"configMap", configMap.Name,
		"namespace", configMap.Namespace,
		"affectedComponents", len(requests))

	return requests
}

// componentReferencesConfigMap checks if a Component's config references the specified ConfigMap.
func (r *ComponentReconciler) componentReferencesConfigMap(
	component *deploymentsv1alpha1.Component,
	configMapNamespace, configMapName string) bool {

	// Parse config to check sources
	var config ConfigReaderConfig
	if err := json.Unmarshal(component.Spec.Config.Raw, &config); err != nil {
		// Skip components with invalid config (they'll fail separately)
		return false
	}

	// Check if any source matches
	for _, source := range config.Sources {
		if source.Namespace == configMapNamespace && source.Name == configMapName {
			return true
		}
	}

	return false
}
