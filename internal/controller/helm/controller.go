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
	"github.com/rinswind/deployment-operator/handler/base"
)

//+kubebuilder:rbac:groups=deployments.deployment-orchestrator.io,resources=components,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=deployments.deployment-orchestrator.io,resources=components/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=deployments.deployment-orchestrator.io,resources=components/finalizers,verbs=update

// ComponentReconciler reconciles a Component object for helm handler using the generic
// controller base with Helm-specific operations factory.
//
// This embeds the base controller directly, eliminating unnecessary delegation
// while maintaining protocol compliance and using the factory pattern for
// efficient configuration parsing.
type ComponentReconciler struct {
	*base.ComponentReconciler
}

// NewComponentReconciler creates a new Helm Component controller with the generic base using factory pattern
func NewComponentReconciler() *ComponentReconciler {
	operationsFactory := &HelmOperationsFactory{}
	config := base.DefaultComponentHandlerConfig("helm")

	return &ComponentReconciler{
		ComponentReconciler: base.NewComponentReconciler(operationsFactory, config),
	}
}
