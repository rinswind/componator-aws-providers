// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package configreader

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Operations Integration", func() {
	var (
		ctx        context.Context
		fakeClient client.Client
		operations *ConfigReaderOperations
		scheme     *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
	})

	Describe("Deploy", func() {
		It("should read ConfigMap and export single value", func() {
			// Create test ConfigMap
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Data: map[string]string{
					"key1": "value1",
				},
			}
			Expect(fakeClient.Create(ctx, configMap)).To(Succeed())

			// Setup operations
			config := &ConfigReaderConfig{
				Sources: []ConfigMapSource{
					{
						Name:      "test-config",
						Namespace: "default",
						Exports: []ExportMapping{
							{Key: "key1"},
						},
					},
				},
			}
			operations = &ConfigReaderOperations{
				config:    config,
				status:    make(ConfigReaderStatus),
				apiReader: fakeClient,
			}

			// Execute Deploy
			result, err := operations.Apply(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.PermanentFailure).NotTo(HaveOccurred())
			Expect(operations.status).To(HaveLen(1))
			Expect(operations.status["key1"]).To(Equal("value1"))
		})

		It("should rename exported value using 'as' field", func() {
			// Create test ConfigMap
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Data: map[string]string{
					"longKeyName": "value1",
				},
			}
			Expect(fakeClient.Create(ctx, configMap)).To(Succeed())

			// Setup operations with renaming
			config := &ConfigReaderConfig{
				Sources: []ConfigMapSource{
					{
						Name:      "test-config",
						Namespace: "default",
						Exports: []ExportMapping{
							{Key: "longKeyName", As: "short"},
						},
					},
				},
			}
			operations = &ConfigReaderOperations{
				config:    config,
				status:    make(ConfigReaderStatus),
				apiReader: fakeClient,
			}

			// Execute Deploy
			result, err := operations.Apply(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.PermanentFailure).NotTo(HaveOccurred())
			Expect(operations.status["short"]).To(Equal("value1"))
			Expect(operations.status).NotTo(HaveKey("longKeyName"))
		})

		It("should read multiple ConfigMaps with multiple exports", func() {
			// Create test ConfigMaps
			configMap1 := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config1",
					Namespace: "ns1",
				},
				Data: map[string]string{
					"key1": "value1",
					"key2": "value2",
				},
			}
			configMap2 := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config2",
					Namespace: "ns2",
				},
				Data: map[string]string{
					"key3": "value3",
				},
			}
			Expect(fakeClient.Create(ctx, configMap1)).To(Succeed())
			Expect(fakeClient.Create(ctx, configMap2)).To(Succeed())

			// Setup operations
			config := &ConfigReaderConfig{
				Sources: []ConfigMapSource{
					{
						Name:      "config1",
						Namespace: "ns1",
						Exports: []ExportMapping{
							{Key: "key1"},
							{Key: "key2", As: "renamed2"},
						},
					},
					{
						Name:      "config2",
						Namespace: "ns2",
						Exports: []ExportMapping{
							{Key: "key3"},
						},
					},
				},
			}
			operations = &ConfigReaderOperations{
				config:    config,
				status:    make(ConfigReaderStatus),
				apiReader: fakeClient,
			}

			// Execute Deploy
			result, err := operations.Apply(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.PermanentFailure).NotTo(HaveOccurred())
			Expect(operations.status).To(HaveLen(3))
			Expect(operations.status["key1"]).To(Equal("value1"))
			Expect(operations.status["renamed2"]).To(Equal("value2"))
			Expect(operations.status["key3"]).To(Equal("value3"))
		})

		It("should fail when ConfigMap does not exist", func() {
			// Setup operations without creating ConfigMap
			config := &ConfigReaderConfig{
				Sources: []ConfigMapSource{
					{
						Name:      "nonexistent",
						Namespace: "default",
						Exports: []ExportMapping{
							{Key: "key1"},
						},
					},
				},
			}
			operations = &ConfigReaderOperations{
				config:    config,
				status:    make(ConfigReaderStatus),
				apiReader: fakeClient,
			}

			// Execute Deploy
			result, err := operations.Apply(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.PermanentFailure).To(HaveOccurred())
			Expect(result.PermanentFailure.Error()).To(ContainSubstring("failed to read ConfigMap"))
		})

		It("should fail when ConfigMap key does not exist", func() {
			// Create test ConfigMap with different keys
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Data: map[string]string{
					"otherKey": "value",
				},
			}
			Expect(fakeClient.Create(ctx, configMap)).To(Succeed())

			// Setup operations
			config := &ConfigReaderConfig{
				Sources: []ConfigMapSource{
					{
						Name:      "test-config",
						Namespace: "default",
						Exports: []ExportMapping{
							{Key: "missingKey"},
						},
					},
				},
			}
			operations = &ConfigReaderOperations{
				config:    config,
				status:    make(ConfigReaderStatus),
				apiReader: fakeClient,
			}

			// Execute Deploy
			result, err := operations.Apply(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.PermanentFailure).To(HaveOccurred())
			Expect(result.PermanentFailure.Error()).To(ContainSubstring("key \"missingKey\" not found"))
			Expect(result.PermanentFailure.Error()).To(ContainSubstring("available keys"))
		})

		It("should serialize status to JSON correctly", func() {
			// Create test ConfigMap
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Data: map[string]string{
					"key1": "value1",
					"key2": "value2",
				},
			}
			Expect(fakeClient.Create(ctx, configMap)).To(Succeed())

			// Setup operations
			config := &ConfigReaderConfig{
				Sources: []ConfigMapSource{
					{
						Name:      "test-config",
						Namespace: "default",
						Exports: []ExportMapping{
							{Key: "key1"},
							{Key: "key2"},
						},
					},
				},
			}
			operations = &ConfigReaderOperations{
				config:    config,
				status:    make(ConfigReaderStatus),
				apiReader: fakeClient,
			}

			// Execute Deploy
			result, err := operations.Apply(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.PermanentFailure).NotTo(HaveOccurred())

			// Verify JSON serialization
			var deserializedStatus ConfigReaderStatus
			Expect(json.Unmarshal(result.UpdatedStatus, &deserializedStatus)).To(Succeed())
			Expect(deserializedStatus["key1"]).To(Equal("value1"))
			Expect(deserializedStatus["key2"]).To(Equal("value2"))
		})
	})

	Describe("CheckDeployment", func() {
		It("should return success immediately", func() {
			operations = &ConfigReaderOperations{
				config: &ConfigReaderConfig{},
				status: make(ConfigReaderStatus),
			}

			result, err := operations.CheckApplied(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Complete).To(BeTrue())
		})
	})

	Describe("Delete", func() {
		It("should return success immediately", func() {
			operations = &ConfigReaderOperations{
				config: &ConfigReaderConfig{},
				status: make(ConfigReaderStatus),
			}

			result, err := operations.Delete(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.PermanentFailure).NotTo(HaveOccurred())
		})
	})

	Describe("CheckDeletion", func() {
		It("should return success immediately", func() {
			operations = &ConfigReaderOperations{
				config: &ConfigReaderConfig{},
				status: make(ConfigReaderStatus),
			}

			result, err := operations.CheckDeleted(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Complete).To(BeTrue())
		})
	})
})
