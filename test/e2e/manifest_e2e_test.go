//go:build e2e
// +build e2e

// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	manifestcontroller "github.com/rinswind/componator-providers/internal/controller/manifest"
	deploymentsv1alpha1 "github.com/rinswind/componator/api/core/v1beta1"
)

var _ = Describe("Manifest Handler E2E", Ordered, func() {
	var (
		manifestCtx       context.Context
		manifestCancel    context.CancelFunc
		manifestK8sClient client.Client
		manifestMgr       manager.Manager
		testNamespace     string
	)

	// Run the Manifest handler controller locally against a real Kubernetes cluster
	// Tests full manifest lifecycle: creation, status updates, and cleanup
	//
	// Prerequisites:
	// - Kubernetes cluster accessible via kubeconfig
	// - CRDs must be installed in cluster
	BeforeAll(func() {
		manifestCtx, manifestCancel = context.WithCancel(context.Background())
		testNamespace = "manifest-test-" + time.Now().Format("20060102-150405")

		By("setting up Kubernetes client using current kubeconfig")
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		Expect(err).NotTo(HaveOccurred(), "Failed to build kubeconfig")

		// Register our API types with the scheme
		err = deploymentsv1alpha1.AddToScheme(scheme.Scheme)
		Expect(err).NotTo(HaveOccurred(), "Failed to add deployment API to scheme")

		// Create Kubernetes client
		manifestK8sClient, err = client.New(config, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred(), "Failed to create Kubernetes client")

		By("creating test namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		Expect(manifestK8sClient.Create(manifestCtx, ns)).To(Succeed(), "Failed to create test namespace")

		By("setting up controller manager for Manifest handler")
		manifestMgr, err = manager.New(config, manager.Options{
			Scheme: scheme.Scheme,
			Logger: zap.New(zap.UseDevMode(true)),
		})
		Expect(err).NotTo(HaveOccurred(), "Failed to create controller manager")

		By("registering Manifest controller")
		manifestReconciler, err := manifestcontroller.NewComponentReconciler(manifestMgr)
		Expect(err).NotTo(HaveOccurred(), "Failed to create Manifest controller")

		err = manifestReconciler.SetupWithManager(manifestMgr)
		Expect(err).NotTo(HaveOccurred(), "Failed to setup Manifest controller")

		By("starting Manifest controller manager")
		go func() {
			defer GinkgoRecover()
			err := manifestMgr.Start(manifestCtx)
			Expect(err).NotTo(HaveOccurred(), "Failed to start controller manager")
		}()

		// Wait for manager to be ready
		Eventually(func() bool {
			return manifestMgr.GetCache().WaitForCacheSync(manifestCtx)
		}, 30*time.Second, time.Second).Should(BeTrue(), "Controller manager cache should sync")
	})

	// Clean up controller manager and test namespace
	AfterAll(func() {
		By("stopping Manifest controller manager")
		manifestCancel()

		By("deleting test namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		_ = manifestK8sClient.Delete(manifestCtx, ns)
	})

	// Clean up individual test resources if test fails
	AfterEach(func() {
		By("cleaning up test components")
		componentList := &deploymentsv1alpha1.ComponentList{}
		if err := manifestK8sClient.List(manifestCtx, componentList, client.InNamespace(testNamespace)); err == nil {
			for i := range componentList.Items {
				if componentList.Items[i].Spec.Type == "manifest" {
					_ = manifestK8sClient.Delete(manifestCtx, &componentList.Items[i])
				}
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(2 * time.Second)

	Context("Basic Manifest Deployment", func() {
		It("should deploy a ConfigMap and Secret successfully", func() {
			By("creating a Component resource with ConfigMap and Secret manifests")

			manifestConfig := map[string]interface{}{
				"manifests": []map[string]interface{}{
					{
						"apiVersion": "v1",
						"kind":       "ConfigMap",
						"metadata": map[string]interface{}{
							"name":      "test-config",
							"namespace": testNamespace,
						},
						"data": map[string]interface{}{
							"key1": "value1",
							"key2": "value2",
						},
					},
					{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name":      "test-secret",
							"namespace": testNamespace,
						},
						"stringData": map[string]interface{}{
							"password": "secretvalue",
						},
					},
				},
			}

			configBytes, err := json.Marshal(manifestConfig)
			Expect(err).NotTo(HaveOccurred(), "Failed to marshal manifest config")

			manifestComponent := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-manifests",
					Namespace: testNamespace,
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "test-manifest-component",
					Handler: "manifest",
					Config: &apiextensionsv1.JSON{
						Raw: configBytes,
					},
				},
			}

			Expect(manifestK8sClient.Create(manifestCtx, manifestComponent)).To(Succeed(), "Failed to create Manifest Component")

			By("waiting for Component to be claimed by Manifest handler")
			Eventually(func(g Gomega) {
				var component deploymentsv1alpha1.Component
				err := manifestK8sClient.Get(manifestCtx, types.NamespacedName{
					Name:      "test-manifests",
					Namespace: testNamespace,
				}, &component)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(component.Finalizers).To(ContainElement(ContainSubstring("manifest.deployment-orchestrator.io")), "Component should be claimed by Manifest handler")
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			By("waiting for Component to reach Ready status")
			Eventually(func(g Gomega) {
				var component deploymentsv1alpha1.Component
				err := manifestK8sClient.Get(manifestCtx, types.NamespacedName{
					Name:      "test-manifests",
					Namespace: testNamespace,
				}, &component)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(component.Status.Phase).To(Equal(deploymentsv1alpha1.ComponentPhaseReady), "Component should reach Ready phase")

				// Verify handler status contains applied resources
				g.Expect(Component.Status.ProviderStatus).NotTo(BeNil(), "HandlerStatus should be populated")

				var manifestStatus map[string]interface{}
				err = json.Unmarshal(Component.Status.ProviderStatus.Raw, &manifestStatus)
				g.Expect(err).NotTo(HaveOccurred(), "Should be able to parse manifest status")
				g.Expect(manifestStatus).To(HaveKey("appliedResources"), "Status should include appliedResources")

				appliedResources := manifestStatus["appliedResources"].([]interface{})
				g.Expect(appliedResources).To(HaveLen(2), "Should have 2 applied resources")
			}, 1*time.Minute, 2*time.Second).Should(Succeed())

			By("verifying ConfigMap and Secret were actually created")
			var configMap corev1.ConfigMap
			err = manifestK8sClient.Get(manifestCtx, types.NamespacedName{
				Name:      "test-config",
				Namespace: testNamespace,
			}, &configMap)
			Expect(err).NotTo(HaveOccurred(), "ConfigMap should exist")
			Expect(configMap.Data["key1"]).To(Equal("value1"), "ConfigMap should have correct data")

			var secret corev1.Secret
			err = manifestK8sClient.Get(manifestCtx, types.NamespacedName{
				Name:      "test-secret",
				Namespace: testNamespace,
			}, &secret)
			Expect(err).NotTo(HaveOccurred(), "Secret should exist")
			Expect(string(secret.Data["password"])).To(Equal("secretvalue"), "Secret should have correct data")

			By("cleaning up the test component")
			Expect(manifestK8sClient.Delete(manifestCtx, manifestComponent)).To(Succeed(), "Failed to delete test component")

			By("waiting for Component deletion to complete")
			Eventually(func(g Gomega) {
				var component deploymentsv1alpha1.Component
				err := manifestK8sClient.Get(manifestCtx, types.NamespacedName{
					Name:      "test-manifests",
					Namespace: testNamespace,
				}, &component)
				g.Expect(err).To(HaveOccurred(), "Component should be deleted")
			}, 1*time.Minute, 2*time.Second).Should(Succeed())

			By("verifying ConfigMap and Secret were deleted")
			Eventually(func(g Gomega) {
				var cm corev1.ConfigMap
				err := manifestK8sClient.Get(manifestCtx, types.NamespacedName{
					Name:      "test-config",
					Namespace: testNamespace,
				}, &cm)
				g.Expect(err).To(HaveOccurred(), "ConfigMap should be deleted")

				var sec corev1.Secret
				err = manifestK8sClient.Get(manifestCtx, types.NamespacedName{
					Name:      "test-secret",
					Namespace: testNamespace,
				}, &sec)
				g.Expect(err).To(HaveOccurred(), "Secret should be deleted")
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})
	})

	Context("Error Handling", func() {
		It("should fail gracefully with invalid manifest", func() {
			By("creating a Component with invalid manifest (missing required fields)")

			manifestConfig := map[string]interface{}{
				"manifests": []map[string]interface{}{
					{
						// Missing apiVersion, kind, and name
						"metadata": map[string]interface{}{
							"namespace": testNamespace,
						},
					},
				},
			}

			configBytes, err := json.Marshal(manifestConfig)
			Expect(err).NotTo(HaveOccurred(), "Failed to marshal manifest config")

			manifestComponent := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-invalid-manifest",
					Namespace: testNamespace,
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "invalid-manifest",
					Handler: "manifest",
					Config: &apiextensionsv1.JSON{
						Raw: configBytes,
					},
				},
			}

			Expect(manifestK8sClient.Create(manifestCtx, manifestComponent)).To(Succeed(), "Failed to create Component")

			By("waiting for Component to reach Failed status")
			Eventually(func(g Gomega) {
				var component deploymentsv1alpha1.Component
				err := manifestK8sClient.Get(manifestCtx, types.NamespacedName{
					Name:      "test-invalid-manifest",
					Namespace: testNamespace,
				}, &component)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(component.Status.Phase).To(Equal(deploymentsv1alpha1.ComponentPhaseFailed), "Component should reach Failed phase")
			}, 1*time.Minute, 2*time.Second).Should(Succeed())

			By("cleaning up the test component")
			Expect(manifestK8sClient.Delete(manifestCtx, manifestComponent)).To(Succeed())
		})
	})
})
