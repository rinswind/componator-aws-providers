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

package controller

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

// ComponentHandlerPredicate creates a predicate that filters Components by handler type.
// This implements the Resource Discovery Phase from the claiming protocol.
func ComponentHandlerPredicate(handlerName string) predicate.Predicate {
	isOurComponent := func(obj client.Object) bool {
		component, ok := obj.(*deploymentsv1alpha1.Component)
		return ok && component.Spec.Handler == handlerName
	}

	return predicate.Funcs{
		CreateFunc:  func(e event.CreateEvent) bool { return isOurComponent(e.Object) },
		UpdateFunc:  func(e event.UpdateEvent) bool { return isOurComponent(e.ObjectNew) },
		DeleteFunc:  func(e event.DeleteEvent) bool { return isOurComponent(e.Object) },
		GenericFunc: func(e event.GenericEvent) bool { return isOurComponent(e.Object) },
	}
}
