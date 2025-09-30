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

package rds

import (
	"time"

	"github.com/rinswind/deployment-operator/handler/base"
)

//+kubebuilder:rbac:groups=deployments.deployment-orchestrator.io,resources=components,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=deployments.deployment-orchestrator.io,resources=components/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=deployments.deployment-orchestrator.io,resources=components/finalizers,verbs=update

// ComponentReconciler reconciles a Component object for rds handler using the generic
// controller base with RDS-specific operations.
//
// This embeds the base controller directly, eliminating unnecessary delegation
// while maintaining protocol compliance.
type ComponentReconciler struct {
	*base.ComponentReconciler
}

// NewComponentReconciler creates a new RDS Component controller with the generic base
func NewComponentReconciler() *ComponentReconciler {
	factory := NewRdsOperationsFactory()

	config := base.DefaultComponentHandlerConfig("rds")
	config.ErrorRequeue = 15 * time.Second
	config.DefaultRequeue = 30 * time.Second
	config.StatusCheckRequeue = 30 * time.Second

	return &ComponentReconciler{base.NewComponentReconciler(factory, config)}
}
