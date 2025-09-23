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

// claiming_protocol_test.go contains tests for the Helm controller's claiming protocol implementation.

package helm

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

var _ = Describe("Helm Controller - Claiming Protocol", func() {
	Context("Claiming Protocol", func() {
		It("should claim an unclaimed helm component", func() {
			// Create a test component with valid config
			configJSON := `{
				"repository": {
					"url": "https://charts.bitnami.com/bitnami",
					"name": "bitnami"
				},
				"chart": {
					"name": "nginx",
					"version": "15.4.4"
				}
			}`

			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-claim-component",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "test-component",
					Handler: "helm",
					Config:  &apiextensionsv1.JSON{Raw: []byte(configJSON)},
				},
				Status: deploymentsv1alpha1.ComponentStatus{
					Phase: deploymentsv1alpha1.ComponentPhasePending,
				},
			}

			// Create component in test environment
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			// Create reconciler
			reconciler := &ComponentReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// Test reconciliation
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-claim-component",
					Namespace: "default",
				},
			}

			// First reconciliation should claim the component
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Refresh component to check status
			var updatedComponent deploymentsv1alpha1.Component
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-claim-component",
				Namespace: "default",
			}, &updatedComponent)).To(Succeed())

			// Verify claiming
			Expect(updatedComponent.Finalizers).To(ContainElement("helm.deployment-orchestrator.io/lifecycle"))
			Expect(updatedComponent.Status.Phase).To(Equal(deploymentsv1alpha1.ComponentPhaseClaimed))
			Expect(updatedComponent.Status.ClaimedBy).To(Equal("helm"))
			Expect(updatedComponent.Status.ClaimedAt).NotTo(BeNil())

			// Cleanup
			Expect(k8sClient.Delete(ctx, &updatedComponent)).To(Succeed())
		})

		It("should skip components already claimed by different handlers", func() {
			// Create a test component already claimed by rds handler
			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-claimed-component",
					Namespace:  "default",
					Finalizers: []string{"rds.deployment-orchestrator.io/lifecycle"},
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "test-component",
					Handler: "helm", // This is for helm but already claimed by rds
				},
			}

			// Create component in test environment
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			// Create reconciler
			reconciler := &ComponentReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// Test reconciliation
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-claimed-component",
					Namespace: "default",
				},
			}

			// Reconciliation should skip claiming
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Refresh component to verify no changes
			var updatedComponent deploymentsv1alpha1.Component
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-claimed-component",
				Namespace: "default",
			}, &updatedComponent)).To(Succeed())

			// Should still only have rds finalizer
			Expect(updatedComponent.Finalizers).To(ContainElement("rds.deployment-orchestrator.io/lifecycle"))
			Expect(updatedComponent.Finalizers).NotTo(ContainElement("helm.deployment-orchestrator.io/lifecycle"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &updatedComponent)).To(Succeed())
		})

		It("should handle already claimed components by same handler", func() {
			// Create a test component already claimed by helm handler
			configJSON := `{
				"repository": {
					"url": "https://charts.bitnami.com/bitnami",
					"name": "bitnami"
				},
				"chart": {
					"name": "nginx",
					"version": "15.4.4"
				}
			}`

			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-owned-component",
					Namespace:  "default",
					Finalizers: []string{"helm.deployment-orchestrator.io/lifecycle"},
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "test-component",
					Handler: "helm",
					Config:  &apiextensionsv1.JSON{Raw: []byte(configJSON)},
				},
				Status: deploymentsv1alpha1.ComponentStatus{
					Phase:     deploymentsv1alpha1.ComponentPhaseClaimed,
					ClaimedBy: "helm",
				},
			}

			// Create component in test environment
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			// Create reconciler
			reconciler := &ComponentReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// Test reconciliation
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-owned-component",
					Namespace: "default",
				},
			}

			// Reconciliation should proceed with already claimed component
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Cleanup
			Expect(k8sClient.Delete(ctx, component)).To(Succeed())
		})
	})
})
