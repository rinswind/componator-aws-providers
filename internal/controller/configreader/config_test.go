// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package configreader

import (
	"context"
	"encoding/json"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestConfigReader(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ConfigReader Suite")
}

var _ = Describe("Config Parsing", func() {
	ctx := context.Background()

	Describe("resolveConfigReaderConfig", func() {
		It("should parse valid config with single source", func() {
			rawConfig := json.RawMessage(`{
				"sources": [
					{
						"name": "test-cm",
						"namespace": "default",
						"exports": [
							{"key": "foo"}
						]
					}
				]
			}`)

			config, err := resolveConfigReaderConfig(ctx, rawConfig)
			Expect(err).NotTo(HaveOccurred())
			Expect(config.Sources).To(HaveLen(1))
			Expect(config.Sources[0].Name).To(Equal("test-cm"))
			Expect(config.Sources[0].Namespace).To(Equal("default"))
			Expect(config.Sources[0].Exports).To(HaveLen(1))
			Expect(config.Sources[0].Exports[0].Key).To(Equal("foo"))
			Expect(config.Sources[0].Exports[0].As).To(BeEmpty())
		})

		It("should parse config with export renaming", func() {
			rawConfig := json.RawMessage(`{
				"sources": [
					{
						"name": "test-cm",
						"namespace": "default",
						"exports": [
							{"key": "longKeyName", "as": "short"}
						]
					}
				]
			}`)

			config, err := resolveConfigReaderConfig(ctx, rawConfig)
			Expect(err).NotTo(HaveOccurred())
			Expect(config.Sources[0].Exports[0].Key).To(Equal("longKeyName"))
			Expect(config.Sources[0].Exports[0].As).To(Equal("short"))
		})

		It("should parse config with multiple sources and exports", func() {
			rawConfig := json.RawMessage(`{
				"sources": [
					{
						"name": "cm1",
						"namespace": "ns1",
						"exports": [
							{"key": "key1"},
							{"key": "key2", "as": "renamed"}
						]
					},
					{
						"name": "cm2",
						"namespace": "ns2",
						"exports": [
							{"key": "key3"}
						]
					}
				]
			}`)

			config, err := resolveConfigReaderConfig(ctx, rawConfig)
			Expect(err).NotTo(HaveOccurred())
			Expect(config.Sources).To(HaveLen(2))
			Expect(config.Sources[0].Exports).To(HaveLen(2))
			Expect(config.Sources[1].Exports).To(HaveLen(1))
		})

		It("should fail on missing sources field", func() {
			rawConfig := json.RawMessage(`{}`)

			_, err := resolveConfigReaderConfig(ctx, rawConfig)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("validation failed"))
		})

		It("should fail on empty sources array", func() {
			rawConfig := json.RawMessage(`{"sources": []}`)

			_, err := resolveConfigReaderConfig(ctx, rawConfig)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("validation failed"))
		})

		It("should fail on source missing name", func() {
			rawConfig := json.RawMessage(`{
				"sources": [
					{
						"namespace": "default",
						"exports": [{"key": "foo"}]
					}
				]
			}`)

			_, err := resolveConfigReaderConfig(ctx, rawConfig)
			Expect(err).To(HaveOccurred())
		})

		It("should fail on source missing namespace", func() {
			rawConfig := json.RawMessage(`{
				"sources": [
					{
						"name": "test-cm",
						"exports": [{"key": "foo"}]
					}
				]
			}`)

			_, err := resolveConfigReaderConfig(ctx, rawConfig)
			Expect(err).To(HaveOccurred())
		})

		It("should fail on source with empty exports", func() {
			rawConfig := json.RawMessage(`{
				"sources": [
					{
						"name": "test-cm",
						"namespace": "default",
						"exports": []
					}
				]
			}`)

			_, err := resolveConfigReaderConfig(ctx, rawConfig)
			Expect(err).To(HaveOccurred())
		})

		It("should fail on export missing key", func() {
			rawConfig := json.RawMessage(`{
				"sources": [
					{
						"name": "test-cm",
						"namespace": "default",
						"exports": [{"as": "renamed"}]
					}
				]
			}`)

			_, err := resolveConfigReaderConfig(ctx, rawConfig)
			Expect(err).To(HaveOccurred())
		})

		It("should fail on invalid JSON", func() {
			rawConfig := json.RawMessage(`{invalid}`)

			_, err := resolveConfigReaderConfig(ctx, rawConfig)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse"))
		})
	})

	Describe("resolveConfigReaderStatus", func() {
		It("should parse empty status", func() {
			rawStatus := json.RawMessage(``)

			status, err := resolveConfigReaderStatus(ctx, rawStatus)
			Expect(err).NotTo(HaveOccurred())
			Expect(status).To(BeEmpty())
		})

		It("should parse status with values", func() {
			rawStatus := json.RawMessage(`{"key1": "value1", "key2": "value2"}`)

			status, err := resolveConfigReaderStatus(ctx, rawStatus)
			Expect(err).NotTo(HaveOccurred())
			Expect(status).To(HaveLen(2))
			Expect(status["key1"]).To(Equal("value1"))
			Expect(status["key2"]).To(Equal("value2"))
		})

		It("should fail on invalid JSON", func() {
			rawStatus := json.RawMessage(`{invalid}`)

			_, err := resolveConfigReaderStatus(ctx, rawStatus)
			Expect(err).To(HaveOccurred())
		})
	})
})
