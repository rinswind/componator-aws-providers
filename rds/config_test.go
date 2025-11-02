// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package rds

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRdsConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RDS Config Suite")
}

var _ = Describe("RDS Config Parsing", func() {
	Describe("resolveSpec", func() {
		It("should parse valid config with managed password defaults", func() {
			rawConfig := json.RawMessage(`{
				"instanceID": "test-db",
				"databaseEngine": "postgres",
				"engineVersion": "14.7",
				"instanceClass": "db.t3.micro",
				"databaseName": "testdb",
				"region": "us-west-2",
				"allocatedStorage": 20,
				"masterUsername": "admin"
			}`)

			var config RdsConfig
			err := json.Unmarshal(rawConfig, &config)
			Expect(err).NotTo(HaveOccurred())

			err = resolveSpec(&config)
			Expect(err).NotTo(HaveOccurred())
			Expect(config.ManageMasterUserPassword).NotTo(BeNil())
			Expect(*config.ManageMasterUserPassword).To(BeTrue())
			Expect(config.MasterUsername).To(Equal("admin"))
		})

		It("should parse config with explicit managed password true", func() {
			rawConfig := json.RawMessage(`{
				"instanceID": "test-db",
				"databaseEngine": "postgres",
				"engineVersion": "14.7",
				"instanceClass": "db.t3.micro",
				"databaseName": "testdb",
				"region": "us-west-2",
				"allocatedStorage": 20,
				"masterUsername": "admin",
				"manageMasterUserPassword": true
			}`)

			var config RdsConfig
			err := json.Unmarshal(rawConfig, &config)
			Expect(err).NotTo(HaveOccurred())

			err = resolveSpec(&config)
			Expect(err).NotTo(HaveOccurred())
			Expect(config.ManageMasterUserPassword).NotTo(BeNil())
			Expect(*config.ManageMasterUserPassword).To(BeTrue())
		})

		It("should parse config with KMS key for secret encryption", func() {
			rawConfig := json.RawMessage(`{
				"instanceID": "test-db",
				"databaseEngine": "postgres",
				"engineVersion": "14.7",
				"instanceClass": "db.t3.micro",
				"databaseName": "testdb",
				"region": "us-west-2",
				"allocatedStorage": 20,
				"masterUsername": "admin",
				"manageMasterUserPassword": true,
				"masterUserSecretKmsKeyId": "arn:aws:kms:us-west-2:123456789012:key/12345678-1234-1234-1234-123456789012"
			}`)

			var config RdsConfig
			err := json.Unmarshal(rawConfig, &config)
			Expect(err).NotTo(HaveOccurred())

			err = resolveSpec(&config)
			Expect(err).NotTo(HaveOccurred())
			Expect(config.MasterUserSecretKmsKeyId).NotTo(BeEmpty())
		})

		It("should fail on explicit manageMasterUserPassword false", func() {
			rawConfig := json.RawMessage(`{
				"instanceID": "test-db",
				"databaseEngine": "postgres",
				"engineVersion": "14.7",
				"instanceClass": "db.t3.micro",
				"databaseName": "testdb",
				"region": "us-west-2",
				"allocatedStorage": 20,
				"masterUsername": "admin",
				"manageMasterUserPassword": false
			}`)

			var config RdsConfig
			err := json.Unmarshal(rawConfig, &config)
			Expect(err).NotTo(HaveOccurred())

			err = resolveSpec(&config)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("manageMasterUserPassword must be true"))
		})

		It("should fail on missing masterUsername", func() {
			rawConfig := json.RawMessage(`{
				"instanceID": "test-db",
				"databaseEngine": "postgres",
				"engineVersion": "14.7",
				"instanceClass": "db.t3.micro",
				"databaseName": "testdb",
				"region": "us-west-2",
				"allocatedStorage": 20
			}`)

			var config RdsConfig
			err := json.Unmarshal(rawConfig, &config)
			Expect(err).NotTo(HaveOccurred())

			err = resolveSpec(&config)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("masterUsername is required"))
		})
	})
})
