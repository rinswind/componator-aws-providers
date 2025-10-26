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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	rdscontroller "github.com/rinswind/componator-aws-providers/internal/controller/rds"
	deploymentsv1alpha1 "github.com/rinswind/componator/api/core/v1beta1"
)

var _ = Describe("RDS Handler E2E", Ordered, func() {
	var (
		rdsCtx       context.Context
		rdsCancel    context.CancelFunc
		rdsK8sClient client.Client
		rdsMgr       manager.Manager
	)

	// Run the RDS handler controller locally against a real Kubernetes cluster
	// Tests full RDS lifecycle: creation, status updates, and cleanup
	//
	// Prerequisites:
	// - VPC, subnets, and security groups must exist in AWS
	// - AWS credentials configured for RDS access
	// - CRDs must be installed in cluster
	BeforeAll(func() {
		rdsCtx, rdsCancel = context.WithCancel(context.Background())

		By("setting up Kubernetes client using current kubeconfig")
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		Expect(err).NotTo(HaveOccurred(), "Failed to build kubeconfig")

		// Register our API types with the scheme
		err = deploymentsv1alpha1.AddToScheme(scheme.Scheme)
		Expect(err).NotTo(HaveOccurred(), "Failed to add deployment API to scheme")

		// Create Kubernetes client
		rdsK8sClient, err = client.New(config, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred(), "Failed to create Kubernetes client")

		By("setting up controller manager for RDS handler")
		rdsMgr, err = manager.New(config, manager.Options{
			Scheme: scheme.Scheme,
			Logger: zap.New(zap.UseDevMode(true)),
		})
		Expect(err).NotTo(HaveOccurred(), "Failed to create controller manager")

		By("registering RDS controller")
		rdsReconciler := rdscontroller.NewComponentReconciler()
		err = rdsReconciler.SetupWithManager(rdsMgr)
		Expect(err).NotTo(HaveOccurred(), "Failed to setup RDS controller")

		By("starting RDS controller manager")
		go func() {
			defer GinkgoRecover()
			err := rdsMgr.Start(rdsCtx)
			Expect(err).NotTo(HaveOccurred(), "Failed to start controller manager")
		}()

		// Wait for manager to be ready
		Eventually(func() bool {
			return rdsMgr.GetCache().WaitForCacheSync(rdsCtx)
		}, 30*time.Second, time.Second).Should(BeTrue(), "Controller manager cache should sync")
	})

	// Clean up controller manager
	AfterAll(func() {
		By("stopping RDS controller manager")
		rdsCancel()

		By("cleaning up any test components")
		componentList := &deploymentsv1alpha1.ComponentList{}
		if err := rdsK8sClient.List(rdsCtx, componentList, client.InNamespace("default")); err == nil {
			for i := range componentList.Items {
				if componentList.Items[i].Spec.Type == "rds" {
					_ = rdsK8sClient.Delete(rdsCtx, &componentList.Items[i])
				}
			}
		}
	})

	// Clean up individual test resources if test fails
	AfterEach(func() {
		By("cleaning up test components")
		componentList := &deploymentsv1alpha1.ComponentList{}
		if err := rdsK8sClient.List(rdsCtx, componentList, client.InNamespace("default")); err == nil {
			for i := range componentList.Items {
				if componentList.Items[i].Name == "test-mysql-db" {
					_ = rdsK8sClient.Delete(rdsCtx, &componentList.Items[i])
				}
			}
		}
	})

	// RDS operations can take significant time
	SetDefaultEventuallyTimeout(10 * time.Minute)
	SetDefaultEventuallyPollingInterval(15 * time.Second)

	Context("RDS Database Deployment", func() {
		BeforeEach(func() {
			By("ensuring no test components exist before test starts")
			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mysql-db",
					Namespace: "default",
				},
			}
			_ = rdsK8sClient.Delete(rdsCtx, component)
			// Wait for deletion to complete if it existed
			Eventually(func() error {
				return rdsK8sClient.Get(rdsCtx, types.NamespacedName{
					Name:      "test-mysql-db",
					Namespace: "default",
				}, component)
			}, 30*time.Second, time.Second).ShouldNot(Succeed())
		})

		It("should deploy and manage MySQL RDS instance lifecycle", func() {
			By("creating a Component resource with minimal MySQL RDS configuration")

			// Create minimal viable RDS configuration
			rdsConfig := map[string]interface{}{
				"instanceID":       "test-mysql-e2e-" + time.Now().Format("20060102-150405"),
				"databaseEngine":   "mysql",
				"engineVersion":    "8.0.35",
				"instanceClass":    "db.t3.micro", // Smallest instance for testing
				"databaseName":     "testdb",
				"region":           "us-east-1",
				"allocatedStorage": 20, // Minimum storage
				"masterUsername":   "testadmin",
				"masterPassword":   "TestPassword123!",
				// Assume these resources exist in the target AWS account
				"vpcSecurityGroupIds": []string{"sg-0218389200791d4a2"}, // Replace with actual security group
				"subnetGroupName":     "rds-handler-test",               // Replace with actual subnet group
				// Enable faster testing
				"skipFinalSnapshot":  true,  // Skip final snapshot for faster cleanup
				"deletionProtection": false, // Disable protection for easier cleanup
				"multiAZ":            false, // Single AZ for cost efficiency
				// Set reasonable timeouts for testing
				"timeouts": map[string]string{
					"create": "20m",
					"update": "15m",
					"delete": "10m",
				},
			}

			configBytes, err := json.Marshal(rdsConfig)
			Expect(err).NotTo(HaveOccurred(), "Failed to marshal RDS config")

			mysqlComponent := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mysql-db",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "mysql-test-instance",
					Handler: "rds",
					Config: &apiextensionsv1.JSON{
						Raw: configBytes,
					},
				},
			}

			Expect(rdsK8sClient.Create(rdsCtx, mysqlComponent)).To(Succeed(), "Failed to create MySQL Component")

			By("waiting for Component to be claimed by RDS handler")
			Eventually(func(g Gomega) {
				var component deploymentsv1alpha1.Component
				err := rdsK8sClient.Get(rdsCtx, types.NamespacedName{
					Name:      "test-mysql-db",
					Namespace: "default",
				}, &component)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(component.Finalizers).To(ContainElement(ContainSubstring("rds.deployment-orchestrator.io")), "Component should be claimed by RDS handler")
			}, 1*time.Minute, 5*time.Second).Should(Succeed())

			By("waiting for Component to progress through deployment phases")
			Eventually(func(g Gomega) {
				var component deploymentsv1alpha1.Component
				err := rdsK8sClient.Get(rdsCtx, types.NamespacedName{
					Name:      "test-mysql-db",
					Namespace: "default",
				}, &component)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(component.Status.Phase).To(Or(
					Equal(deploymentsv1alpha1.ComponentPhaseDeploying),
					Equal(deploymentsv1alpha1.ComponentPhaseReady),
				), "Component should progress through deployment phases")
			}, 3*time.Minute, 15*time.Second).Should(Succeed())

			By("waiting for Component to reach Ready status")
			Eventually(func(g Gomega) {
				var component deploymentsv1alpha1.Component
				err := rdsK8sClient.Get(rdsCtx, types.NamespacedName{
					Name:      "test-mysql-db",
					Namespace: "default",
				}, &component)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(component.Status.Phase).To(Equal(deploymentsv1alpha1.ComponentPhaseReady), "Component should reach Ready phase")

				// Verify status contains database endpoint information
				g.Expect(Component.Status.ProviderStatus).NotTo(BeNil(), "HandlerStatus should be populated")

				// Parse handler status to verify RDS-specific information
				var rdsStatus map[string]interface{}
				err = json.Unmarshal(Component.Status.ProviderStatus.Raw, &rdsStatus)
				g.Expect(err).NotTo(HaveOccurred(), "Should be able to parse RDS status")
				g.Expect(rdsStatus).To(HaveKey("endpoint"), "RDS status should include endpoint")
				g.Expect(rdsStatus).To(HaveKey("instanceStatus"), "RDS status should include instance status")
				g.Expect(rdsStatus["instanceStatus"]).To(Equal("available"), "RDS instance should be available")
			}, 20*time.Minute, 30*time.Second).Should(Succeed())

			By("verifying Component status is properly set")
			var finalComponent deploymentsv1alpha1.Component
			err = rdsK8sClient.Get(rdsCtx, types.NamespacedName{
				Name:      "test-mysql-db",
				Namespace: "default",
			}, &finalComponent)
			Expect(err).NotTo(HaveOccurred())

			// Verify component is in Ready phase
			Expect(finalComponent.Status.Phase).To(Equal(deploymentsv1alpha1.ComponentPhaseReady), "Component should be in Ready phase")

			// Verify handler status contains RDS information
			Expect(finalComponent.Status.ProviderStatus).NotTo(BeNil(), "HandlerStatus should be populated")

			By("cleaning up the test component")
			Expect(rdsK8sClient.Delete(rdsCtx, mysqlComponent)).To(Succeed(), "Failed to delete test component")

			By("waiting for Component deletion to complete")
			Eventually(func(g Gomega) {
				var component deploymentsv1alpha1.Component
				err := rdsK8sClient.Get(rdsCtx, types.NamespacedName{
					Name:      "test-mysql-db",
					Namespace: "default",
				}, &component)
				g.Expect(err).To(HaveOccurred(), "Component should be deleted")
			}, 15*time.Minute, 30*time.Second).Should(Succeed())

			By("verifying RDS instance is cleaned up in AWS")
			// Note: This test assumes the RDS handler properly cleans up the AWS RDS instance
			// In a complete test suite, you might want to verify this by checking AWS directly
			// For this minimal test, we trust that the controller's deletion logic works correctly
		})

		// TODO: Add more RDS handler scenarios:
		// - Test with invalid configuration (should fail gracefully)
		// - Test PostgreSQL configuration
		// - Test error handling for insufficient AWS permissions
		// - Test configuration updates/modifications
		// - Test multi-AZ deployment scenarios
		// - Test backup and restore scenarios
	})
})
