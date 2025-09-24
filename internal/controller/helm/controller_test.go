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

// controller_test.go contains tests for the Helm controller's reconciliation logic.
// Configuration parsing tests are in config_test.go for better organization.

package helm

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/rinswind/deployment-operator/handler/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

var _ = Describe("Helm Controller", func() {
	Context("When reconciling a Component", func() {
		It("should handle helm components with valid config", func() {
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
					Name:      "test-helm-component",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "test-component",
					Handler: "helm",
					Config:  &apiextensionsv1.JSON{Raw: []byte(configJSON)},
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
					Name:      "test-helm-component",
					Namespace: "default",
				},
			}

			result, err := reconciler.Reconcile(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// Cleanup
			Expect(k8sClient.Delete(ctx, component)).To(Succeed())
		})

		It("should fail with helm components that have invalid config", func() {
			// Create a test component without config
			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-helm-component-no-config",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "test-component",
					Handler: "helm",
					// Config is nil
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
					Name:      "test-helm-component-no-config",
					Namespace: "default",
				},
			}

			// First reconciliation should claim the component
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Check that component is claimed after first reconciliation
			var claimedComponent deploymentsv1alpha1.Component
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-helm-component-no-config",
				Namespace: "default",
			}, &claimedComponent)).To(Succeed())

			Expect(claimedComponent.Finalizers).To(ContainElement(util.MakeHandlerFinalizer(HandlerName)))
			Expect(claimedComponent.Status.Phase).To(Equal(deploymentsv1alpha1.ComponentPhaseClaimed))
			Expect(claimedComponent.Status.ClaimedBy).To(Equal(HandlerName))

			// Second reconciliation should fail on config validation
			_, err = reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred()) // Controller should handle errors gracefully by setting status

			// Check that status was updated to Failed after second reconciliation
			var updatedComponent deploymentsv1alpha1.Component
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-helm-component-no-config",
				Namespace: "default",
			}, &updatedComponent)).To(Succeed())

			// Should be claimed but failed
			Expect(updatedComponent.Finalizers).To(ContainElement(util.MakeHandlerFinalizer(HandlerName)))
			Expect(updatedComponent.Status.Phase).To(Equal(deploymentsv1alpha1.ComponentPhaseFailed))
			Expect(updatedComponent.Status.Message).To(ContainSubstring("Configuration error"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &updatedComponent)).To(Succeed())
		})

		It("should ignore non-helm components", func() {
			// Create a test component with different handler
			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rds-component",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "test-component",
					Handler: "rds",
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
					Name:      "test-rds-component",
					Namespace: "default",
				},
			}

			result, err := reconciler.Reconcile(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// Cleanup
			Expect(k8sClient.Delete(ctx, component)).To(Succeed())
		})

		It("should handle component not found gracefully", func() {
			// Create reconciler
			reconciler := &ComponentReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// Test reconciliation for non-existent component
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "non-existent-component",
					Namespace: "default",
				},
			}

			result, err := reconciler.Reconcile(ctx, req)

			// Should not return error (client.IgnoreNotFound)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})
	})
})
