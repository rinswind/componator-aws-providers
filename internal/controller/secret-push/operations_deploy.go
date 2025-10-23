// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package secretpush

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/rinswind/deployment-operator/componentkit/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Deploy initiates secret generation and push to AWS Secrets Manager
func (op *SecretPushOperations) Deploy(ctx context.Context) (*controller.ActionResult, error) {
	log := logf.FromContext(ctx).WithValues("secretName", op.config.SecretName)

	log.Info("Starting secret-push deployment")

	// Build secret data: generate passwords and combine with static fields
	secretData, generatedCount, staticCount, err := op.buildSecretData(ctx)
	if err != nil {
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to build secret data: %w", err), awsErrorClassifier)
	}

	// Marshal to JSON (flat key-value structure)
	secretJSON, err := json.Marshal(secretData)
	if err != nil {
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to marshal secret data: %w", err), awsErrorClassifier)
	}

	// Check if secret exists
	existingArn, _, err := op.findSecret(ctx, op.config.SecretName)
	if err != nil {
		return controller.ActionResultForError(op.status, err, awsErrorClassifier)
	}

	var secretArn, versionId string
	var details string

	if existingArn == "" {
		// Create new secret
		secretArn, versionId, err = op.createSecret(ctx, secretJSON)
		if err != nil {
			return controller.ActionResultForError(op.status, err, awsErrorClassifier)
		}

		details = fmt.Sprintf("Created secret %s with %d fields (%d generated, %d static)",
			op.config.SecretName, generatedCount+staticCount, generatedCount, staticCount)
	} else if op.config.UpdatePolicy == UpdatePolicyIfNotExists {
		// Secret exists and update policy is IfNotExists - skip update
		log.Info("Secret exists and updatePolicy is IfNotExists, skipping update")
		secretArn = existingArn

		// Keep existing versionId empty - we're not modifying the secret
		details = fmt.Sprintf("Secret %s exists, update skipped (IfNotExists policy)", op.config.SecretName)
	} else {
		// Update existing secret
		secretArn, versionId, err = op.updateSecret(ctx, secretJSON)
		if err != nil {
			return controller.ActionResultForError(op.status, err, awsErrorClassifier)
		}

		details = fmt.Sprintf("Updated secret %s with %d fields (%d generated, %d static)",
			op.config.SecretName, generatedCount+staticCount, generatedCount, staticCount)
	}

	// Update status once at the end
	op.status.SecretArn = secretArn
	op.status.SecretName = op.config.SecretName
	op.status.VersionId = versionId
	op.status.Region = op.awsConfig.Region
	op.status.FieldCount = len(op.config.Fields)

	return controller.ActionSuccessWithDetails(op.status, details)
}

// CheckDeployment verifies secret exists and is ready
func (op *SecretPushOperations) CheckDeployment(ctx context.Context) (*controller.CheckResult, error) {
	log := logf.FromContext(ctx).WithValues("secretName", op.config.SecretName)

	// If we don't have a secret ARN yet, deployment hasn't started
	if op.status.SecretArn == "" {
		log.V(1).Info("No secret ARN in status, deployment not started")
		return controller.CheckInProgress(op.status)
	}

	// Verify secret exists
	arn, name, err := op.findSecret(ctx, op.status.SecretArn)

	if err != nil {
		return nil, fmt.Errorf("failed to check secret status: %w", err)
	}

	if arn == "" {
		return controller.CheckResultForError(op.status,
			fmt.Errorf("secret not found at ARN %s", op.status.SecretArn), awsErrorClassifier)
	}

	// Update status with current secret info
	op.status.SecretArn = arn
	op.status.SecretName = name

	log.Info("Secret deployment complete",
		"secretArn", op.status.SecretArn,
		"fieldCount", op.status.FieldCount)

	details := fmt.Sprintf("Secret %s ready with %d fields", op.status.SecretName, op.status.FieldCount)
	return controller.CheckCompleteWithDetails(op.status, details)
}

// buildSecretData generates passwords and combines with static fields
// Returns a flat map suitable for JSON marshaling, plus counts of generated and static fields
func (op *SecretPushOperations) buildSecretData(ctx context.Context) (map[string]interface{}, int, int, error) {
	log := logf.FromContext(ctx)
	secretData := make(map[string]interface{})
	var generatedCount, staticCount int

	for fieldName, fieldSpec := range op.config.Fields {
		if fieldSpec.Value != "" {
			// Static value (already resolved by cross-component templating)
			secretData[fieldName] = fieldSpec.Value
			staticCount++

			log.V(1).Info("Added static field", "field", fieldName)
		} else if fieldSpec.Generator != nil {
			// Generate via AWS GetRandomPassword
			result, err := op.smClient.GetRandomPassword(ctx, &secretsmanager.GetRandomPasswordInput{
				PasswordLength:          aws.Int64(fieldSpec.Generator.PasswordLength),
				RequireEachIncludedType: aws.Bool(fieldSpec.Generator.RequireEachIncludedType),
				ExcludePunctuation:      aws.Bool(fieldSpec.Generator.ExcludePunctuation),
				ExcludeNumbers:          aws.Bool(fieldSpec.Generator.ExcludeNumbers),
				ExcludeLowercase:        aws.Bool(fieldSpec.Generator.ExcludeLowercase),
				ExcludeUppercase:        aws.Bool(fieldSpec.Generator.ExcludeUppercase),
				IncludeSpace:            aws.Bool(fieldSpec.Generator.IncludeSpace),
				ExcludeCharacters:       aws.String(fieldSpec.Generator.ExcludeCharacters),
			})
			if err != nil {
				return nil, 0, 0, fmt.Errorf("failed to generate password for field %s: %w", fieldName, err)
			}
			secretData[fieldName] = aws.ToString(result.RandomPassword)

			generatedCount++
			log.V(1).Info("Generated random password", "field", fieldName, "length", fieldSpec.Generator.PasswordLength)
		}
	}

	log.Info("Built secret data", "totalFields", len(secretData), "generated", generatedCount, "static", staticCount)
	return secretData, generatedCount, staticCount, nil
}

// findSecret checks if the secret exists in AWS Secrets Manager
// Returns: arn, name (empty strings if not found), error
func (op *SecretPushOperations) findSecret(ctx context.Context, secretId string) (string, string, error) {
	describeOutput, err := op.smClient.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
		SecretId: aws.String(secretId),
	})

	if err == nil {
		return aws.ToString(describeOutput.ARN), aws.ToString(describeOutput.Name), nil
	}

	var notFoundErr *types.ResourceNotFoundException
	if errors.As(err, &notFoundErr) {
		return "", "", nil
	}

	return "", "", fmt.Errorf("failed to check secret existence: %w", err)
}

// createSecret creates a new secret in AWS Secrets Manager
// Returns: secretArn, versionId, error
func (op *SecretPushOperations) createSecret(ctx context.Context, secretJSON []byte) (string, string, error) {
	log := logf.FromContext(ctx)

	createInput := &secretsmanager.CreateSecretInput{
		Name:         aws.String(op.config.SecretName),
		SecretString: aws.String(string(secretJSON)),
		Tags: []types.Tag{
			{Key: aws.String("managed-by"), Value: aws.String("deployment-operator")},
			{Key: aws.String("handler"), Value: aws.String(HandlerName)},
		},
	}

	if op.config.KmsKeyId != "" {
		createInput.KmsKeyId = aws.String(op.config.KmsKeyId)
	}

	createOutput, err := op.smClient.CreateSecret(ctx, createInput)
	if err != nil {
		return "", "", fmt.Errorf("failed to create secret: %w", err)
	}

	log.Info("Successfully created secret",
		"secretArn", aws.ToString(createOutput.ARN),
		"versionId", aws.ToString(createOutput.VersionId))

	return aws.ToString(createOutput.ARN), aws.ToString(createOutput.VersionId), nil
}

// updateSecret updates an existing secret in AWS Secrets Manager
// Returns: secretArn, versionId, error
func (op *SecretPushOperations) updateSecret(ctx context.Context, secretJSON []byte) (string, string, error) {
	log := logf.FromContext(ctx)

	updateOutput, err := op.smClient.UpdateSecret(ctx, &secretsmanager.UpdateSecretInput{
		SecretId:     aws.String(op.config.SecretName),
		SecretString: aws.String(string(secretJSON)),
	})

	if err != nil {
		return "", "", fmt.Errorf("failed to update secret: %w", err)
	}

	log.Info("Successfully updated secret",
		"secretArn", aws.ToString(updateOutput.ARN),
		"versionId", aws.ToString(updateOutput.VersionId))

	return aws.ToString(updateOutput.ARN), aws.ToString(updateOutput.VersionId), nil
}
