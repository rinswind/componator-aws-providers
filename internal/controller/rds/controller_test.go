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
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

func TestRDSController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RDS Controller Suite")
}

var _ = Describe("RDS Controller", func() {
	Context("When reconciling a Component", func() {
		It("should handle rds components", func() {
			// Setup scheme
			s := scheme.Scheme
			err := deploymentsv1alpha1.AddToScheme(s)
			Expect(err).NotTo(HaveOccurred())

			// Create a test component
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

			// Create fake client with the component
			client := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(component).
				Build()

			// Create reconciler
			reconciler := &ComponentReconciler{
				Client: client,
				Scheme: s,
			}

			// Test reconciliation
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-rds-component",
					Namespace: "default",
				},
			}

			ctx := context.Background()
			result, err := reconciler.Reconcile(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})

		It("should ignore non-rds components", func() {
			// Setup scheme
			s := scheme.Scheme
			err := deploymentsv1alpha1.AddToScheme(s)
			Expect(err).NotTo(HaveOccurred())

			// Create a test component with different handler
			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-helm-component",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "test-component",
					Handler: "helm",
				},
			}

			// Create fake client with the component
			client := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(component).
				Build()

			// Create reconciler
			reconciler := &ComponentReconciler{
				Client: client,
				Scheme: s,
			}

			// Test reconciliation
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-helm-component",
					Namespace: "default",
				},
			}

			ctx := context.Background()
			result, err := reconciler.Reconcile(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})
	})
})
