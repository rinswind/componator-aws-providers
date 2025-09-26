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
// These tests are focused on the parseHelmConfig() function
// and are separate from the main controller reconciliation tests.

package helm

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

func init() {
	// This file contains configuration-specific tests that are included in the main test suite
}

var _ = Describe("Helm Configuration", func() {
	Context("When parsing Helm configuration", func() {
		It("should parse valid helm configuration", func() {
			// Create component with valid helm config using nested YAML structure
			configJSON := `{
				"repository": {
					"url": "https://charts.bitnami.com/bitnami",
					"name": "bitnami"
				},
				"chart": {
					"name": "nginx",
					"version": "15.4.4"
				},
				"releaseName": "test-nginx",
				"values": {
					"service": {
						"type": "LoadBalancer"
					},
					"replicaCount": 3
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

			config, err := resolveHelmConfig(component)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).NotTo(BeNil())

			Expect(config.Repository.URL).To(Equal("https://charts.bitnami.com/bitnami"))
			Expect(config.Repository.Name).To(Equal("bitnami"))
			Expect(config.Chart.Name).To(Equal("nginx"))
			Expect(config.Chart.Version).To(Equal("15.4.4"))
			Expect(config.Namespace).To(Equal("web"))

			// Test nested values structure
			serviceConfig, exists := config.Values["service"]
			Expect(exists).To(BeTrue())
			serviceMap, ok := serviceConfig.(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(serviceMap).To(HaveKeyWithValue("type", "LoadBalancer"))
			Expect(config.Values).To(HaveKeyWithValue("replicaCount", float64(3))) // JSON numbers are float64
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
				},
				"releaseName": "test-nginx"
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

			config, err := resolveHelmConfig(component)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).NotTo(BeNil())

			Expect(config.Repository.URL).To(Equal("https://charts.bitnami.com/bitnami"))
			Expect(config.Repository.Name).To(Equal("bitnami"))
			Expect(config.Chart.Name).To(Equal("nginx"))
			Expect(config.Chart.Version).To(Equal("15.4.4"))
			Expect(config.Namespace).To(Equal("default")) // Should be resolved from Component namespace
			Expect(config.Values).To(BeEmpty())
		})

		It("should parse configuration with complex nested values", func() {
			// Create component with complex nested values configuration
			configJSON := `{
				"repository": {
					"url": "https://charts.bitnami.com/bitnami",
					"name": "bitnami"
				},
				"chart": {
					"name": "postgresql",
					"version": "12.12.10"
				},
				"releaseName": "test-postgres",
				"values": {
					"auth": {
						"postgresPassword": "mysecretpassword",
						"database": "myapp"
					},
					"persistence": {
						"size": "20Gi"
					},
					"metrics": {
						"enabled": true
					},
					"primary": {
						"resources": {
							"requests": {
								"memory": "256Mi"
							}
						}
					}
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

			config, err := resolveHelmConfig(component)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).NotTo(BeNil())

			Expect(config.Repository.URL).To(Equal("https://charts.bitnami.com/bitnami"))
			Expect(config.Repository.Name).To(Equal("bitnami"))
			Expect(config.Chart.Name).To(Equal("postgresql"))
			Expect(config.Chart.Version).To(Equal("12.12.10"))
			Expect(config.Namespace).To(Equal("database"))

			// Test nested auth values
			authConfig, exists := config.Values["auth"]
			Expect(exists).To(BeTrue())
			authMap, ok := authConfig.(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(authMap).To(HaveKeyWithValue("postgresPassword", "mysecretpassword"))
			Expect(authMap).To(HaveKeyWithValue("database", "myapp"))

			// Test nested persistence values
			persistenceConfig, exists := config.Values["persistence"]
			Expect(exists).To(BeTrue())
			persistenceMap, ok := persistenceConfig.(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(persistenceMap).To(HaveKeyWithValue("size", "20Gi"))

			// Test deeply nested primary.resources.requests.memory
			primaryConfig, exists := config.Values["primary"]
			Expect(exists).To(BeTrue())
			primaryMap, ok := primaryConfig.(map[string]any)
			Expect(ok).To(BeTrue())
			resourcesConfig, exists := primaryMap["resources"]
			Expect(exists).To(BeTrue())
			resourcesMap, ok := resourcesConfig.(map[string]any)
			Expect(ok).To(BeTrue())
			requestsConfig, exists := resourcesMap["requests"]
			Expect(exists).To(BeTrue())
			requestsMap, ok := requestsConfig.(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(requestsMap).To(HaveKeyWithValue("memory", "256Mi"))
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

			config, err := resolveHelmConfig(component)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("config is required for helm components"))
			Expect(config).To(BeNil())
		})

		It("should fail when validation rules are violated", func() {
			// Test that validation framework is enabled by using invalid data
			configJSON := `{
				"repository": {
					"url": "invalid-url-format",
					"name": ""
				},
				"chart": {
					"name": "",
					"version": ""
				},
				"releaseName": ""
			}`

			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-validation-failure",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "validation-failure",
					Handler: "helm",
					Config:  &apiextensionsv1.JSON{Raw: []byte(configJSON)},
				},
			}

			config, err := resolveHelmConfig(component)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("validation failed"))
			Expect(config).To(BeNil())
		})

		It("should resolve target namespace from Component when not specified in config", func() {
			// Test namespace resolution: config has no namespace, should use Component's namespace
			configJSON := `{
				"repository": {
					"url": "https://charts.bitnami.com/bitnami",
					"name": "bitnami"
				},
				"chart": {
					"name": "nginx",
					"version": "15.4.4"
				},
				"releaseName": "test-nginx"
			}`

			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-namespace-resolution",
					Namespace: "production",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "nginx-prod",
					Handler: "helm",
					Config:  &apiextensionsv1.JSON{Raw: []byte(configJSON)},
				},
			}

			config, err := resolveHelmConfig(component)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).NotTo(BeNil())
			Expect(config.Namespace).To(Equal("production")) // Should be resolved from Component namespace
		})

		It("should preserve explicit namespace from config", func() {
			// Test explicit namespace: config specifies namespace, should use that instead of Component's
			configJSON := `{
				"repository": {
					"url": "https://charts.bitnami.com/bitnami",
					"name": "bitnami"
				},
				"chart": {
					"name": "nginx",
					"version": "15.4.4"
				},
				"releaseName": "test-nginx",
				"namespace": "custom-namespace"
			}`

			component := &deploymentsv1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-explicit-namespace",
					Namespace: "default",
				},
				Spec: deploymentsv1alpha1.ComponentSpec{
					Name:    "nginx-custom",
					Handler: "helm",
					Config:  &apiextensionsv1.JSON{Raw: []byte(configJSON)},
				},
			}

			config, err := resolveHelmConfig(component)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).NotTo(BeNil())
			Expect(config.Namespace).To(Equal("custom-namespace")) // Should preserve explicit config value
		})
	})
})
