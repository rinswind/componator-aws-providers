// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package secretpush

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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
	secretData, err := op.buildSecretData(ctx)
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
	describeOutput, err := op.smClient.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
		SecretId: aws.String(op.config.SecretName),
	})

	if err != nil {
		var notFoundErr *types.ResourceNotFoundException
		if errors.As(err, &notFoundErr) {
			// Create new secret
			return op.createSecret(ctx, secretJSON)
		}
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to check secret existence: %w", err), awsErrorClassifier)
	}

	// Secret exists - check update policy
	if op.config.UpdatePolicy == UpdatePolicyIfNotExists {
		log.Info("Secret exists and updatePolicy is IfNotExists, skipping update")
		op.status.SecretArn = aws.ToString(describeOutput.ARN)
		op.status.SecretName = aws.ToString(describeOutput.Name)
		op.status.SecretPath = aws.ToString(describeOutput.Name)
		op.status.Region = op.awsConfig.Region
		op.status.FieldCount = len(op.config.Fields)
		return controller.ActionSuccess(op.status)
	}

	// Update existing secret
	return op.updateSecret(ctx, secretJSON)
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
	describeOutput, err := op.smClient.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
		SecretId: aws.String(op.status.SecretArn),
	})

	if err != nil {
		var notFoundErr *types.ResourceNotFoundException
		if errors.As(err, &notFoundErr) {
			return controller.CheckResultForError(op.status,
				fmt.Errorf("secret not found at ARN %s", op.status.SecretArn), awsErrorClassifier)
		}
		return nil, fmt.Errorf("failed to check secret status: %w", err)
	}

	// Update status with current secret info
	op.status.SecretArn = aws.ToString(describeOutput.ARN)
	op.status.SecretName = aws.ToString(describeOutput.Name)
	op.status.SecretPath = aws.ToString(describeOutput.Name)

	log.Info("Secret deployment complete",
		"secretArn", op.status.SecretArn,
		"fieldCount", op.status.FieldCount)

	return controller.CheckComplete(op.status)
}

// buildSecretData generates passwords and combines with static fields
// Returns a flat map suitable for JSON marshaling
func (op *SecretPushOperations) buildSecretData(ctx context.Context) (map[string]interface{}, error) {
	log := logf.FromContext(ctx)
	secretData := make(map[string]interface{})

	for fieldName, fieldSpec := range op.config.Fields {
		if fieldSpec.Value != "" {
			// Static value (already resolved by cross-component templating)
			secretData[fieldName] = fieldSpec.Value
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
				return nil, fmt.Errorf("failed to generate password for field %s: %w", fieldName, err)
			}
			secretData[fieldName] = aws.ToString(result.RandomPassword)
			log.V(1).Info("Generated random password", "field", fieldName, "length", fieldSpec.Generator.PasswordLength)
		}
	}

	log.Info("Built secret data", "totalFields", len(secretData))
	return secretData, nil
}

// createSecret creates a new secret in AWS Secrets Manager
func (op *SecretPushOperations) createSecret(ctx context.Context, secretJSON []byte) (*controller.ActionResult, error) {
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
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to create secret: %w", err), awsErrorClassifier)
	}

	// Update status with created secret info
	op.status.SecretArn = aws.ToString(createOutput.ARN)
	op.status.SecretName = op.config.SecretName
	op.status.SecretPath = op.config.SecretName
	op.status.VersionId = aws.ToString(createOutput.VersionId)
	op.status.Region = op.awsConfig.Region
	op.status.LastSyncTime = time.Now().UTC().Format(time.RFC3339)
	op.status.FieldCount = len(op.config.Fields)

	log.Info("Successfully created secret",
		"secretArn", op.status.SecretArn,
		"versionId", op.status.VersionId,
		"fieldCount", op.status.FieldCount)

	return controller.ActionSuccess(op.status)
}

// updateSecret updates an existing secret in AWS Secrets Manager
func (op *SecretPushOperations) updateSecret(ctx context.Context, secretJSON []byte) (*controller.ActionResult, error) {
	log := logf.FromContext(ctx)

	updateOutput, err := op.smClient.UpdateSecret(ctx, &secretsmanager.UpdateSecretInput{
		SecretId:     aws.String(op.config.SecretName),
		SecretString: aws.String(string(secretJSON)),
	})

	if err != nil {
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to update secret: %w", err), awsErrorClassifier)
	}

	// Update status with updated secret info
	op.status.SecretArn = aws.ToString(updateOutput.ARN)
	op.status.VersionId = aws.ToString(updateOutput.VersionId)
	op.status.LastSyncTime = time.Now().UTC().Format(time.RFC3339)
	op.status.FieldCount = len(op.config.Fields)

	log.Info("Successfully updated secret",
		"secretArn", op.status.SecretArn,
		"versionId", aws.ToString(updateOutput.VersionId),
		"fieldCount", op.status.FieldCount)

	return controller.ActionSuccess(op.status)
}
