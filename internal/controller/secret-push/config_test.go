// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package secretpush

import (
	"context"
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"
)

func TestResolveSecretPushConfig(t *testing.T) {
	tests := []struct {
		name          string
		configJSON    string
		expectError   bool
		errorContains string
		validate      func(*testing.T, *SecretPushConfig)
	}{
		{
			name: "valid config with generated field",
			configJSON: `{
				"secretName": "test-secret",
				"fields": {
					"password": {
						"generator": {
							"passwordLength": 32,
							"requireEachIncludedType": true
						}
					}
				}
			}`,
			validate: func(t *testing.T, cfg *SecretPushConfig) {
				g := NewWithT(t)
				g.Expect(cfg.SecretName).To(Equal("test-secret"))
				g.Expect(cfg.Fields).To(HaveLen(1))
				g.Expect(cfg.Fields["password"].Generator).ToNot(BeNil())
				g.Expect(cfg.Fields["password"].Generator.PasswordLength).To(Equal(int64(32)))
				g.Expect(cfg.UpdatePolicy).To(Equal(UpdatePolicyIfNotExists)) // Default
				g.Expect(cfg.DeletionPolicy).To(Equal(DeletionPolicyDelete))  // Default
			},
		},
		{
			name: "valid config with static field",
			configJSON: `{
				"secretName": "test-secret",
				"fields": {
					"username": {
						"value": "admin"
					}
				}
			}`,
			validate: func(t *testing.T, cfg *SecretPushConfig) {
				g := NewWithT(t)
				g.Expect(cfg.Fields["username"].Value).To(Equal("admin"))
				g.Expect(cfg.Fields["username"].Generator).To(BeNil())
			},
		},
		{
			name: "valid config with mixed fields",
			configJSON: `{
				"secretName": "test-secret",
				"fields": {
					"username": {"value": "admin"},
					"password": {
						"generator": {
							"passwordLength": 32
						}
					},
					"email": {"value": "admin@example.com"}
				}
			}`,
			validate: func(t *testing.T, cfg *SecretPushConfig) {
				g := NewWithT(t)
				g.Expect(cfg.Fields).To(HaveLen(3))
				g.Expect(cfg.Fields["username"].Value).To(Equal("admin"))
				g.Expect(cfg.Fields["password"].Generator).ToNot(BeNil())
				g.Expect(cfg.Fields["email"].Value).To(Equal("admin@example.com"))
			},
		},
		{
			name: "valid config with all update policies",
			configJSON: `{
				"secretName": "test-secret",
				"fields": {"test": {"value": "val"}},
				"updatePolicy": "AlwaysUpdate",
				"deletionPolicy": "Retain"
			}`,
			validate: func(t *testing.T, cfg *SecretPushConfig) {
				g := NewWithT(t)
				g.Expect(cfg.UpdatePolicy).To(Equal(UpdatePolicyAlwaysUpdate))
				g.Expect(cfg.DeletionPolicy).To(Equal(DeletionPolicyRetain))
			},
		},
		{
			name: "missing secretName",
			configJSON: `{
				"fields": {
					"password": {"generator": {"passwordLength": 32}}
				}
			}`,
			expectError:   true,
			errorContains: "secretName is required",
		},
		{
			name: "missing fields",
			configJSON: `{
				"secretName": "test-secret"
			}`,
			expectError:   true,
			errorContains: "at least one field is required",
		},
		{
			name: "field with both value and generator",
			configJSON: `{
				"secretName": "test-secret",
				"fields": {
					"password": {
						"value": "static",
						"generator": {"passwordLength": 32}
					}
				}
			}`,
			expectError:   true,
			errorContains: "cannot have both value and generator",
		},
		{
			name: "field with neither value nor generator",
			configJSON: `{
				"secretName": "test-secret",
				"fields": {
					"password": {}
				}
			}`,
			expectError:   true,
			errorContains: "must have either value or generator",
		},
		{
			name: "invalid passwordLength - zero",
			configJSON: `{
				"secretName": "test-secret",
				"fields": {
					"password": {
						"generator": {"passwordLength": 0}
					}
				}
			}`,
			expectError:   true,
			errorContains: "passwordLength must be positive",
		},
		{
			name: "invalid passwordLength - exceeds limit",
			configJSON: `{
				"secretName": "test-secret",
				"fields": {
					"password": {
						"generator": {"passwordLength": 5000}
					}
				}
			}`,
			expectError:   true,
			errorContains: "passwordLength cannot exceed 4096",
		},
		{
			name: "invalid updatePolicy",
			configJSON: `{
				"secretName": "test-secret",
				"fields": {"test": {"value": "val"}},
				"updatePolicy": "Invalid"
			}`,
			expectError:   true,
			errorContains: "updatePolicy must be",
		},
		{
			name: "invalid deletionPolicy",
			configJSON: `{
				"secretName": "test-secret",
				"fields": {"test": {"value": "val"}},
				"deletionPolicy": "Invalid"
			}`,
			expectError:   true,
			errorContains: "deletionPolicy must be",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()

			config, err := resolveSecretPushConfig(ctx, json.RawMessage(tt.configJSON))

			if tt.expectError {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.errorContains))
				return
			}

			g.Expect(err).ToNot(HaveOccurred())
			if tt.validate != nil {
				tt.validate(t, config)
			}
		})
	}
}

func TestResolveSecretPushStatus(t *testing.T) {
	tests := []struct {
		name       string
		statusJSON string
		validate   func(*testing.T, *SecretPushStatus)
	}{
		{
			name:       "empty status",
			statusJSON: ``,
			validate: func(t *testing.T, status *SecretPushStatus) {
				g := NewWithT(t)
				g.Expect(status.SecretArn).To(BeEmpty())
				g.Expect(status.FieldCount).To(Equal(0))
			},
		},
		{
			name: "populated status",
			statusJSON: `{
				"secretArn": "arn:aws:secretsmanager:us-east-1:123456:secret:test-AbCdEf",
				"secretName": "test-secret",
				"secretPath": "test-secret",
				"versionId": "uuid-123",
				"region": "us-east-1",
				"fieldCount": 3
			}`,
			validate: func(t *testing.T, status *SecretPushStatus) {
				g := NewWithT(t)
				g.Expect(status.SecretArn).To(Equal("arn:aws:secretsmanager:us-east-1:123456:secret:test-AbCdEf"))
				g.Expect(status.SecretName).To(Equal("test-secret"))
				g.Expect(status.VersionId).To(Equal("uuid-123"))
				g.Expect(status.FieldCount).To(Equal(3))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()

			status, err := resolveSecretPushStatus(ctx, json.RawMessage(tt.statusJSON))

			g.Expect(err).ToNot(HaveOccurred())
			if tt.validate != nil {
				tt.validate(t, status)
			}
		})
	}
}
