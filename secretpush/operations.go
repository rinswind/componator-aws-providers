// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package secretpush

import (
	"context"
	"fmt"

	"github.com/rinswind/componator/componentkit/controller"
	k8stypes "k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// applyAction initiates secret generation and push to AWS Secrets Manager
func applyAction(
	ctx context.Context,
	name k8stypes.NamespacedName,
	spec SecretPushSpec,
	status SecretPushStatus) (*controller.ActionResultFunc[SecretPushStatus], error) {

	// Validate and apply defaults to config
	if err := resolveSpec(&spec); err != nil {
		return controller.ActionFailureFunc(status, fmt.Sprintf("config validation failed: %v", err))
	}

	log := logf.FromContext(ctx).WithValues("secretName", spec.SecretName)
	log.Info("Starting secret-push deployment")

	// Build secret data: generate passwords and combine with static fields
	secretData, generatedCount, staticCount, err := buildSecretData(ctx, spec)
	if err != nil {
		return controller.ActionResultForErrorFunc(status, err, awsErrorClassifier)
	}

	// Check if secret exists
	existingArn, _, err := findSecret(ctx, spec.SecretName)
	if err != nil {
		return controller.ActionResultForErrorFunc(status, err, awsErrorClassifier)
	}

	var secretArn, versionId string
	var details string

	if existingArn == "" {
		// Create new secret
		tags := buildSecretTags(name)
		secretArn, versionId, err = createSecret(ctx, spec.SecretName, secretData, tags, spec.KmsKeyId)
		if err != nil {
			return controller.ActionResultForErrorFunc(status, err, awsErrorClassifier)
		}

		details = fmt.Sprintf("Created secret %s with %d fields (%d generated, %d static)",
			spec.SecretName, generatedCount+staticCount, generatedCount, staticCount)
	} else if spec.UpdatePolicy == UpdatePolicyIfNotExists {
		// Secret exists and update policy is IfNotExists - skip update
		log.Info("Secret exists and updatePolicy is IfNotExists, skipping update")
		secretArn = existingArn

		// Keep existing versionId empty - we're not modifying the secret
		details = fmt.Sprintf("Secret %s exists, update skipped (IfNotExists policy)", spec.SecretName)
	} else {
		// Update existing secret
		secretArn, versionId, err = updateSecret(ctx, spec.SecretName, secretData)
		if err != nil {
			return controller.ActionResultForErrorFunc(status, err, awsErrorClassifier)
		}

		details = fmt.Sprintf("Updated secret %s with %d fields (%d generated, %d static)",
			spec.SecretName, generatedCount+staticCount, generatedCount, staticCount)
	}

	// Update status
	status.SecretArn = secretArn
	status.SecretName = spec.SecretName
	status.VersionId = versionId
	status.Region = awsConfig.Region
	status.FieldCount = len(spec.Fields)

	return controller.ActionSuccessFunc(status, details)
}

// deleteAction implements deletion operations for secrets
func deleteAction(
	ctx context.Context,
	name k8stypes.NamespacedName,
	spec SecretPushSpec,
	status SecretPushStatus) (*controller.ActionResultFunc[SecretPushStatus], error) {

	log := logf.FromContext(ctx).WithValues("secretName", spec.SecretName, "secretArn", status.SecretArn)

	// If no secret ARN in status, nothing to delete
	if status.SecretArn == "" {
		log.Info("No secret ARN in status, nothing to delete")
		return controller.ActionSuccessFunc(status, "No secret to delete")
	}

	// Check deletion policy
	if spec.DeletionPolicy == DeletionPolicyRetain {
		log.Info("DeletionPolicy is Retain, skipping secret deletion", "secretArn", status.SecretArn)
		details := fmt.Sprintf("Secret %s retained (Retain policy)", spec.SecretName)
		return controller.ActionSuccessFunc(status, details)
	}

	log.Info("Starting secret deletion")

	// Delete secret from AWS
	err := deleteSecret(ctx, status.SecretArn)
	if err != nil {
		return controller.ActionResultForErrorFunc(status, err, awsErrorClassifier)
	}

	// Status remains unchanged (no update needed for deletion)
	details := fmt.Sprintf("Deleted secret %s", spec.SecretName)
	return controller.ActionSuccessFunc(status, details)
}

// buildSecretData generates passwords and combines with static fields
// Returns a flat map suitable for JSON marshaling, plus counts of generated and static fields
func buildSecretData(ctx context.Context, spec SecretPushSpec) (map[string]string, int, int, error) {
	log := logf.FromContext(ctx)

	var generatedCount, staticCount int

	secretData := make(map[string]string)

	for fieldName, fieldSpec := range spec.Fields {
		if fieldSpec.Value != "" {
			// Static value (already resolved by cross-component templating)
			secretData[fieldName] = fieldSpec.Value
			staticCount++

			log.V(1).Info("Added static field", "field", fieldName)
		} else if fieldSpec.Generator != nil {
			// Generate via AWS GetRandomPassword
			password, err := getRandomPassword(ctx, fieldSpec.Generator)
			if err != nil {
				return nil, 0, 0, fmt.Errorf("failed to generate password for field %s: %w", fieldName, err)
			}

			secretData[fieldName] = password
			generatedCount++

			log.V(1).Info("Generated random password", "field", fieldName, "length", fieldSpec.Generator.PasswordLength)
		}
	}

	log.Info("Built secret data", "totalFields", len(secretData), "generated", generatedCount, "static", staticCount)
	return secretData, generatedCount, staticCount, nil
}

func buildSecretTags(name k8stypes.NamespacedName) map[string]string {
	return map[string]string{
		"managed-by": "componator",
		"component":  name.Namespace + "/" + name.Name,
	}
}
