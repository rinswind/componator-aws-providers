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
	"context"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
)

func TestHelmController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Helm Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	var err error
	err = deploymentsv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "..", "deployment-operator", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: false,
	}

	// Retrieve the first found binary directory to allow running tests from IDEs
	if getFirstFoundEnvTestBinaryDir() != "" {
		testEnv.BinaryAssetsDirectory = getFirstFoundEnvTestBinaryDir()
	}

	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

// getFirstFoundEnvTestBinaryDir locates the first binary in the specified path.
// ENVTEST-based tests depend on specific binaries, usually located in paths set by
// controller-runtime. When running tests directly (e.g., via an IDE) without using
// Makefile targets, the 'BinaryAssetsDirectory' must be explicitly configured.
//
// This function streamlines the process by finding the required binaries, similar to
// setting the 'KUBEBUILDER_ASSETS' environment variable. To ensure the binaries are
// properly set up, run 'make setup-envtest' beforehand.
func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		logf.Log.Error(err, "Failed to read directory", "path", basePath)
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}

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

			result, err := reconciler.Reconcile(ctx, req)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("config is required for helm components"))
			Expect(result).To(Equal(reconcile.Result{}))

			// Cleanup
			Expect(k8sClient.Delete(ctx, component)).To(Succeed())
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
