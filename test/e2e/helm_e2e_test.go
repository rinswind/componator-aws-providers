//go:build e2e
// +build e2e

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

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	helmcontroller "github.com/rinswind/deployment-operator-handlers/internal/controller/helm"
	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

var _ = Describe("Helm Handler E2E", Ordered, func() {
	var (
		helmCtx       context.Context
		helmCancel    context.CancelFunc
		helmK8sClient client.Client
		helmMgr       manager.Manager
	)

	// Run the Helm handler controller locally against a real Kubernetes cluster
	// This gives us the benefits of integration testing but against real Kubernetes
	BeforeAll(func() {
		helmCtx, helmCancel = context.WithCancel(context.Background())

		By("setting up Kubernetes client using current kubeconfig")
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		Expect(err).NotTo(HaveOccurred(), "Failed to build kubeconfig")

		// Register our API types with the scheme
		err = deploymentsv1alpha1.AddToScheme(scheme.Scheme)
		Expect(err).NotTo(HaveOccurred(), "Failed to add deployment API to scheme")

		// Create Kubernetes client
		helmK8sClient, err = client.New(config, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred(), "Failed to create Kubernetes client")

		By("setting up controller manager for Helm handler")
		helmMgr, err = manager.New(config, manager.Options{
			Scheme: scheme.Scheme,
			Logger: zap.New(zap.UseDevMode(true)),
		})
		Expect(err).NotTo(HaveOccurred(), "Failed to create controller manager")

		By("registering Helm controller")
		err = (&helmcontroller.ComponentReconciler{
			Client: helmMgr.GetClient(),
			Scheme: helmMgr.GetScheme(),
		}).SetupWithManager(helmMgr)
		Expect(err).NotTo(HaveOccurred(), "Failed to setup Helm controller")

		By("starting Helm controller manager")
		go func() {
			defer GinkgoRecover()
			err := helmMgr.Start(helmCtx)
			Expect(err).NotTo(HaveOccurred(), "Failed to start controller manager")
		}()

		// Wait for manager to be ready
		Eventually(func() bool {
			return helmMgr.GetCache().WaitForCacheSync(helmCtx)
		}, 30*time.Second, time.Second).Should(BeTrue(), "Controller manager cache should sync")
	})

	// Clean up controller manager
	AfterAll(func() {
		By("stopping Helm controller manager")
		helmCancel()

		By("cleaning up any test components")
		componentList := &deploymentsv1alpha1.ComponentList{}
		if err := helmK8sClient.List(helmCtx, componentList, client.InNamespace("default")); err == nil {
			for i := range componentList.Items {
				_ = helmK8sClient.Delete(helmCtx, &componentList.Items[i])
			}
		}
	})

	// Clean up individual test resources if test fails
	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			By("cleaning up failed test components")
			componentList := &deploymentsv1alpha1.ComponentList{}
			if err := helmK8sClient.List(helmCtx, componentList, client.InNamespace("default")); err == nil {
				for i := range componentList.Items {
					if componentList.Items[i].Name == "test-nginx" {
						_ = helmK8sClient.Delete(helmCtx, &componentList.Items[i])
					}
				}
			}
		}
	})

	SetDefaultEventuallyTimeout(3 * time.Minute)
	SetDefaultEventuallyPollingInterval(5 * time.Second)

	Context("Helm Chart Deployment", func() {
		BeforeEach(func() {
			By("ensuring no test components exist before test starts")
			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nginx",
					Namespace: "default",
				},
			}
			_ = helmK8sClient.Delete(helmCtx, component)
			// Wait for deletion to complete if it existed
			Eventually(func() error {
				return helmK8sClient.Get(helmCtx, types.NamespacedName{
					Name:      "test-nginx",
					Namespace: "default",
				}, component)
			}, 30*time.Second, time.Second).ShouldNot(Succeed())
		})

		It("should deploy and manage nginx chart lifecycle", func() {
			By("creating a Component resource with nginx chart configuration")
			nginxComponent := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nginx",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "nginx-test",
					Handler: "helm",
					Config: &apiextensionsv1.JSON{
						Raw: []byte(`{
							"repository": {
								"url": "https://repo.broadcom.com/bitnami-files",
								"name": "bitnami"
							},
							"chart": {
								"name": "nginx",
								"version": "18.2.4"
							},
							"values": {
								"service.type": "ClusterIP",
								"replicaCount": 1
							}
						}`),
					},
				},
			}

			Expect(helmK8sClient.Create(helmCtx, nginxComponent)).To(Succeed(), "Failed to create nginx Component")

			By("waiting for Component to be claimed by Helm handler")
			Eventually(func(g Gomega) {
				var component deploymentsv1alpha1.Component
				err := helmK8sClient.Get(helmCtx, types.NamespacedName{
					Name:      "test-nginx",
					Namespace: "default",
				}, &component)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(component.Finalizers).To(ContainElement(ContainSubstring("helm.deployment-orchestrator.io")), "Component should be claimed by Helm handler")
			}, 30*time.Second, time.Second).Should(Succeed())

			By("waiting for Component to reach Ready status")
			Eventually(func(g Gomega) {
				var component deploymentsv1alpha1.Component
				err := helmK8sClient.Get(helmCtx, types.NamespacedName{
					Name:      "test-nginx",
					Namespace: "default",
				}, &component)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(component.Status.Phase).To(Equal(deploymentsv1alpha1.ComponentPhaseReady), "Component should reach Ready phase")
			}, 3*time.Minute, 5*time.Second).Should(Succeed())

			By("verifying nginx Helm release exists")
			Eventually(func(g Gomega) {
				// List nginx pods created by Helm to validate real deployment
				podList := &metav1.PartialObjectMetadataList{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "PodList",
					},
				}
				err := helmK8sClient.List(helmCtx, podList, client.InNamespace("default"), client.MatchingLabels{"app.kubernetes.io/name": "nginx"})
				g.Expect(err).NotTo(HaveOccurred(), "Should be able to list nginx pods")
				g.Expect(podList.Items).To(HaveLen(1), "Should have exactly 1 nginx pod from Helm chart")
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("cleaning up the test component")
			Expect(helmK8sClient.Delete(helmCtx, nginxComponent)).To(Succeed(), "Failed to delete test component")

			By("waiting for Component deletion to complete")
			Eventually(func(g Gomega) {
				var component deploymentsv1alpha1.Component
				err := helmK8sClient.Get(helmCtx, types.NamespacedName{
					Name:      "test-nginx",
					Namespace: "default",
				}, &component)
				g.Expect(err).To(HaveOccurred(), "Component should be deleted")
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("verifying nginx Helm release is cleaned up")
			Eventually(func(g Gomega) {
				podList := &metav1.PartialObjectMetadataList{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "PodList",
					},
				}
				err := helmK8sClient.List(helmCtx, podList, client.InNamespace("default"), client.MatchingLabels{"app.kubernetes.io/name": "nginx"})
				if err == nil {
					g.Expect(podList.Items).To(HaveLen(0), "nginx pods should be cleaned up")
				}
			}, 2*time.Minute, 5*time.Second).Should(Succeed())
		})

		// TODO: Add more Helm handler scenarios:
		// - Test with invalid chart configuration
		// - Test with chart upgrade scenarios
		// - Test with custom values override
		// - Test error handling for unreachable repositories
	})
})
