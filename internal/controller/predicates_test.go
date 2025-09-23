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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

func TestComponentHandlerPredicate(t *testing.T) {
	tests := []struct {
		name           string
		handlerName    string
		component      *deploymentsv1alpha1.Component
		expectFiltered bool
	}{
		{
			name:        "matching handler should be accepted",
			handlerName: "helm",
			component: &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-component",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Handler: "helm",
				},
			},
			expectFiltered: true,
		},
		{
			name:        "non-matching handler should be rejected",
			handlerName: "helm",
			component: &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-component",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Handler: "rds",
				},
			},
			expectFiltered: false,
		},
		{
			name:        "empty handler should be rejected",
			handlerName: "helm",
			component: &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-component",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Handler: "",
				},
			},
			expectFiltered: false,
		},
		{
			name:        "rds handler filtering works correctly",
			handlerName: "rds",
			component: &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-component",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Handler: "rds",
				},
			},
			expectFiltered: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := ComponentHandlerPredicate(tt.handlerName)

			// Test CreateEvent
			createResult := predicate.Create(event.CreateEvent{
				Object: tt.component,
			})
			if createResult != tt.expectFiltered {
				t.Errorf("CreateEvent filter failed: expected %v, got %v", tt.expectFiltered, createResult)
			}

			// Test UpdateEvent  
			updateResult := predicate.Update(event.UpdateEvent{
				ObjectOld: &deploymentsv1alpha1.Component{},
				ObjectNew: tt.component,
			})
			if updateResult != tt.expectFiltered {
				t.Errorf("UpdateEvent filter failed: expected %v, got %v", tt.expectFiltered, updateResult)
			}

			// Test DeleteEvent
			deleteResult := predicate.Delete(event.DeleteEvent{
				Object: tt.component,
			})
			if deleteResult != tt.expectFiltered {
				t.Errorf("DeleteEvent filter failed: expected %v, got %v", tt.expectFiltered, deleteResult)
			}

			// Test GenericEvent
			genericResult := predicate.Generic(event.GenericEvent{
				Object: tt.component,
			})
			if genericResult != tt.expectFiltered {
				t.Errorf("GenericEvent filter failed: expected %v, got %v", tt.expectFiltered, genericResult)
			}
		})
	}
}

func TestComponentHandlerPredicateReusability(t *testing.T) {
	// Test that the factory function creates independent predicates
	helmPredicate := ComponentHandlerPredicate("helm")
	rdsPredicate := ComponentHandlerPredicate("rds")

	helmComponent := &deploymentsv1alpha1.Component{
		Spec: deploymentsv1alpha1.ComponentSpec{Handler: "helm"},
	}

	rdsComponent := &deploymentsv1alpha1.Component{
		Spec: deploymentsv1alpha1.ComponentSpec{Handler: "rds"},
	}

	// Helm predicate should accept helm, reject rds
	if !helmPredicate.Create(event.CreateEvent{Object: helmComponent}) {
		t.Error("Helm predicate should accept helm component")
	}
	if helmPredicate.Create(event.CreateEvent{Object: rdsComponent}) {
		t.Error("Helm predicate should reject rds component")
	}

	// RDS predicate should accept rds, reject helm
	if !rdsPredicate.Create(event.CreateEvent{Object: rdsComponent}) {
		t.Error("RDS predicate should accept rds component")
	}
	if rdsPredicate.Create(event.CreateEvent{Object: helmComponent}) {
		t.Error("RDS predicate should reject helm component")
	}
}
