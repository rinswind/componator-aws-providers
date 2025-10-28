// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package rds

import (
	"context"
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
	ctx := context.Background()

	Describe("resolveRdsConfig", func() {
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

			config, err := resolveRdsConfig(ctx, rawConfig)
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

			config, err := resolveRdsConfig(ctx, rawConfig)
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

			config, err := resolveRdsConfig(ctx, rawConfig)
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

			_, err := resolveRdsConfig(ctx, rawConfig)
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

			_, err := resolveRdsConfig(ctx, rawConfig)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("masterUsername is required"))
		})
	})

	Describe("resolveRdsStatus", func() {
		It("should parse empty status", func() {
			rawStatus := json.RawMessage(``)

			status, err := resolveRdsStatus(ctx, rawStatus)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.MasterUserSecretArn).To(BeEmpty())
		})

		It("should parse status with secret ARN", func() {
			rawStatus := json.RawMessage(`{
				"instanceStatus": "available",
				"instanceARN": "arn:aws:rds:us-west-2:123456789012:db:test-db",
				"endpoint": "test-db.abc123.us-west-2.rds.amazonaws.com",
				"port": 5432,
				"masterUserSecretArn": "arn:aws:secretsmanager:us-west-2:123456789012:secret:rds!db-abc123"
			}`)

			status, err := resolveRdsStatus(ctx, rawStatus)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.MasterUserSecretArn).To(Equal("arn:aws:secretsmanager:us-west-2:123456789012:secret:rds!db-abc123"))
		})
	})
})
