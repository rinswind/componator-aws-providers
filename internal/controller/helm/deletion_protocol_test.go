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

// deletion_protocol_test.go contains tests for the Helm controller's deletion protocol implementation.

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

var _ = Describe("Helm Controller - Deletion Protocol", func() {
	Context("Deletion Protocol", func() {
		It("should wait for coordination finalizer removal", func() {
			// Create a test component with coordination finalizer
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
					Name:      "test-deletion-component",
					Namespace: "default",
					Finalizers: []string{
						"helm.deployment-orchestrator.io/lifecycle",
						"composition.deployment-orchestrator.io/coordination",
					},
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

			// Mark component for deletion
			Expect(k8sClient.Delete(ctx, component)).To(Succeed())

			// Create reconciler
			reconciler := &ComponentReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// Test reconciliation
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-deletion-component",
					Namespace: "default",
				},
			}

			// Should wait for coordination finalizer removal
			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			// Component should still have both finalizers
			var updatedComponent deploymentsv1alpha1.Component
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-deletion-component",
				Namespace: "default",
			}, &updatedComponent)).To(Succeed())

			Expect(updatedComponent.Finalizers).To(ContainElement("helm.deployment-orchestrator.io/lifecycle"))
			Expect(updatedComponent.Finalizers).To(ContainElement("composition.deployment-orchestrator.io/coordination"))

			// Cleanup by removing finalizers manually
			updatedComponent.Finalizers = []string{}
			Expect(k8sClient.Update(ctx, &updatedComponent)).To(Succeed())
		})

		It("should proceed with cleanup when coordination finalizer is removed", func() {
			// Create a test component without coordination finalizer
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
					Name:       "test-cleanup-component",
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

			// Mark component for deletion
			Expect(k8sClient.Delete(ctx, component)).To(Succeed())

			// Create reconciler
			reconciler := &ComponentReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// Test reconciliation
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-cleanup-component",
					Namespace: "default",
				},
			}

			// Should proceed with cleanup
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Component should have finalizer removed and be deleted
			var updatedComponent deploymentsv1alpha1.Component
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-cleanup-component",
				Namespace: "default",
			}, &updatedComponent)

			// Component should be deleted or have no finalizers
			if err == nil {
				Expect(updatedComponent.Finalizers).NotTo(ContainElement("helm.deployment-orchestrator.io/lifecycle"))
			}
		})
	})
})
