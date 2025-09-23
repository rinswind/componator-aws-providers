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

// config_test.go contains tests for Helm configuration parsing and validation.
// These tests are focused on the parseHelmConfig() and generateReleaseName() functions
// and are separate from the main controller reconciliation tests.

package helm

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

func init() {
	// This file contains configuration-specific tests that are included in the main test suite
}

var _ = Describe("Helm Configuration", func() {
	Context("When parsing Helm configuration", func() {
		var reconciler *ComponentReconciler

		BeforeEach(func() {
			// Setup scheme
			s := scheme.Scheme
			err := deploymentsv1alpha1.AddToScheme(s)
			Expect(err).NotTo(HaveOccurred())

			// Create fake client
			client := fake.NewClientBuilder().
				WithScheme(s).
				Build()

			reconciler = &ComponentReconciler{
				Client: client,
				Scheme: s,
			}
		})

		It("should parse valid helm configuration", func() {
			// Create component with valid helm config
			configJSON := `{
				"repository": {
					"url": "https://charts.bitnami.com/bitnami",
					"name": "bitnami"
				},
				"chart": {
					"name": "nginx",
					"version": "15.4.4"
				},
				"values": {
					"service.type": "LoadBalancer",
					"replicaCount": "3"
				},
				"namespace": "web"
			}`

			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nginx",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "nginx-app",
					Handler: "helm",
					Config:  &apiextensionsv1.JSON{Raw: []byte(configJSON)},
				},
			}

			config, err := reconciler.parseHelmConfig(component)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).NotTo(BeNil())

			Expect(config.Repository.URL).To(Equal("https://charts.bitnami.com/bitnami"))
			Expect(config.Repository.Name).To(Equal("bitnami"))
			Expect(config.Chart.Name).To(Equal("nginx"))
			Expect(config.Chart.Version).To(Equal("15.4.4"))
			Expect(config.Namespace).To(Equal("web"))
			Expect(config.Values).To(HaveKeyWithValue("service.type", "LoadBalancer"))
			Expect(config.Values).To(HaveKeyWithValue("replicaCount", "3"))
		})

		It("should parse minimal helm configuration", func() {
			// Create component with minimal valid helm config (no optional fields)
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
					Name:      "test-nginx-minimal",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "nginx-minimal",
					Handler: "helm",
					Config:  &apiextensionsv1.JSON{Raw: []byte(configJSON)},
				},
			}

			config, err := reconciler.parseHelmConfig(component)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).NotTo(BeNil())

			Expect(config.Repository.URL).To(Equal("https://charts.bitnami.com/bitnami"))
			Expect(config.Repository.Name).To(Equal("bitnami"))
			Expect(config.Chart.Name).To(Equal("nginx"))
			Expect(config.Chart.Version).To(Equal("15.4.4"))
			Expect(config.Namespace).To(BeEmpty())
			Expect(config.Values).To(BeEmpty())
		})

		It("should parse configuration with complex values", func() {
			// Create component with complex values configuration
			configJSON := `{
				"repository": {
					"url": "https://charts.bitnami.com/bitnami",
					"name": "bitnami"
				},
				"chart": {
					"name": "postgresql",
					"version": "12.12.10"
				},
				"values": {
					"auth.postgresPassword": "mysecretpassword",
					"auth.database": "myapp",
					"persistence.size": "20Gi",
					"metrics.enabled": "true",
					"primary.resources.requests.memory": "256Mi"
				},
				"namespace": "database"
			}`

			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-postgresql",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "postgresql-db",
					Handler: "helm",
					Config:  &apiextensionsv1.JSON{Raw: []byte(configJSON)},
				},
			}

			config, err := reconciler.parseHelmConfig(component)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).NotTo(BeNil())

			Expect(config.Repository.URL).To(Equal("https://charts.bitnami.com/bitnami"))
			Expect(config.Repository.Name).To(Equal("bitnami"))
			Expect(config.Chart.Name).To(Equal("postgresql"))
			Expect(config.Chart.Version).To(Equal("12.12.10"))
			Expect(config.Namespace).To(Equal("database"))
			Expect(config.Values).To(HaveKeyWithValue("auth.postgresPassword", "mysecretpassword"))
			Expect(config.Values).To(HaveKeyWithValue("auth.database", "myapp"))
			Expect(config.Values).To(HaveKeyWithValue("persistence.size", "20Gi"))
			Expect(config.Values).To(HaveKeyWithValue("metrics.enabled", "true"))
			Expect(config.Values).To(HaveKeyWithValue("primary.resources.requests.memory", "256Mi"))
		})

		It("should fail when config is nil", func() {
			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-no-config",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "no-config",
					Handler: "helm",
					Config:  nil,
				},
			}

			config, err := reconciler.parseHelmConfig(component)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("config is required for helm components"))
			Expect(config).To(BeNil())
		})

		It("should fail when config JSON is invalid", func() {
			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-invalid-json",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "invalid-json",
					Handler: "helm",
					Config:  &apiextensionsv1.JSON{Raw: []byte(`{invalid json`)},
				},
			}

			config, err := reconciler.parseHelmConfig(component)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse helm config"))
			Expect(config).To(BeNil())
		})

		It("should fail when repository URL is missing", func() {
			configJSON := `{
				"repository": {
					"name": "bitnami"
				},
				"chart": {
					"name": "nginx",
					"version": "15.4.4"
				}
			}`

			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-no-url",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "no-url",
					Handler: "helm",
					Config:  &apiextensionsv1.JSON{Raw: []byte(configJSON)},
				},
			}

			config, err := reconciler.parseHelmConfig(component)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("repository.url is required"))
			Expect(config).To(BeNil())
		})

		It("should fail when repository name is missing", func() {
			configJSON := `{
				"repository": {
					"url": "https://charts.bitnami.com/bitnami"
				},
				"chart": {
					"name": "nginx",
					"version": "15.4.4"
				}
			}`

			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-no-repo-name",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "no-repo-name",
					Handler: "helm",
					Config:  &apiextensionsv1.JSON{Raw: []byte(configJSON)},
				},
			}

			config, err := reconciler.parseHelmConfig(component)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("repository.name is required"))
			Expect(config).To(BeNil())
		})

		It("should fail when chart name is missing", func() {
			configJSON := `{
				"repository": {
					"url": "https://charts.bitnami.com/bitnami",
					"name": "bitnami"
				},
				"chart": {
					"version": "15.4.4"
				}
			}`

			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-no-chart-name",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "no-chart-name",
					Handler: "helm",
					Config:  &apiextensionsv1.JSON{Raw: []byte(configJSON)},
				},
			}

			config, err := reconciler.parseHelmConfig(component)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("chart.name is required"))
			Expect(config).To(BeNil())
		})

		It("should fail when chart version is missing", func() {
			configJSON := `{
				"repository": {
					"url": "https://charts.bitnami.com/bitnami",
					"name": "bitnami"
				},
				"chart": {
					"name": "nginx"
				}
			}`

			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-no-chart-version",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "no-chart-version",
					Handler: "helm",
					Config:  &apiextensionsv1.JSON{Raw: []byte(configJSON)},
				},
			}

			config, err := reconciler.parseHelmConfig(component)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("chart.version is required"))
			Expect(config).To(BeNil())
		})
	})

	Context("When generating release names", func() {
		var reconciler *ComponentReconciler

		BeforeEach(func() {
			reconciler = &ComponentReconciler{}
		})

		It("should generate deterministic release names", func() {
			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-app",
					Namespace: "production",
				},
			}

			releaseName := reconciler.generateReleaseName(component)
			Expect(releaseName).To(Equal("production-my-app"))

			// Should be deterministic
			releaseName2 := reconciler.generateReleaseName(component)
			Expect(releaseName2).To(Equal(releaseName))
		})

		It("should handle different namespaces", func() {
			component1 := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-app",
					Namespace: "staging",
				},
			}

			component2 := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-app",
					Namespace: "production",
				},
			}

			releaseName1 := reconciler.generateReleaseName(component1)
			releaseName2 := reconciler.generateReleaseName(component2)

			Expect(releaseName1).To(Equal("staging-my-app"))
			Expect(releaseName2).To(Equal("production-my-app"))
			Expect(releaseName1).NotTo(Equal(releaseName2))
		})

		It("should handle special characters properly", func() {
			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "web-frontend",
					Namespace: "prod-env",
				},
			}

			releaseName := reconciler.generateReleaseName(component)
			Expect(releaseName).To(Equal("prod-env-web-frontend"))
		})

		It("should ensure unique names for same component in different namespaces", func() {
			components := []*deploymentsv1alpha1.Component{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "api-service",
						Namespace: "development",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "api-service",
						Namespace: "staging",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "api-service",
						Namespace: "production",
					},
				},
			}

			releaseNames := make([]string, len(components))
			for i, component := range components {
				releaseNames[i] = reconciler.generateReleaseName(component)
			}

			// All release names should be unique
			Expect(releaseNames[0]).To(Equal("development-api-service"))
			Expect(releaseNames[1]).To(Equal("staging-api-service"))
			Expect(releaseNames[2]).To(Equal("production-api-service"))

			// Ensure all are different
			Expect(releaseNames[0]).NotTo(Equal(releaseNames[1]))
			Expect(releaseNames[1]).NotTo(Equal(releaseNames[2]))
			Expect(releaseNames[0]).NotTo(Equal(releaseNames[2]))
		})
	})
})
