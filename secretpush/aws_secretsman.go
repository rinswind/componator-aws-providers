// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package secretpush

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/rinswind/componator/componentkit/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Package-level singletons initialized during registration
var (
	awsConfig aws.Config
	smClient  *secretsmanager.Client
)

// findSecret checks if the secret exists in AWS Secrets Manager
// Returns: arn, name (empty strings if not found), error
func findSecret(ctx context.Context, id string) (string, string, error) {
	describeOutput, err := smClient.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
		SecretId: aws.String(id),
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
func createSecret(
	ctx context.Context, name string, value map[string]string, tags map[string]string, kmsKeyId string) (string, string, error) {

	log := logf.FromContext(ctx)

	valueJSON, err := json.Marshal(value)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal secret data: %s", err)
	}

	tagsList := make([]types.Tag, 0, len(tags))
	for k, v := range tags {
		tagsList = append(tagsList, types.Tag{Key: aws.String(k), Value: aws.String(v)})
	}

	createInput := &secretsmanager.CreateSecretInput{
		Name:         aws.String(name),
		SecretString: aws.String(string(valueJSON)),
		Tags:         tagsList,
	}

	if kmsKeyId != "" {
		createInput.KmsKeyId = aws.String(kmsKeyId)
	}

	createOutput, err := smClient.CreateSecret(ctx, createInput)
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
func updateSecret(ctx context.Context, name string, value map[string]string) (string, string, error) {
	log := logf.FromContext(ctx)

	valueJSON, err := json.Marshal(value)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal secret data: %s", err)
	}

	updateOutput, err := smClient.UpdateSecret(ctx, &secretsmanager.UpdateSecretInput{
		SecretId:     aws.String(name),
		SecretString: aws.String(string(valueJSON)),
	})

	if err != nil {
		return "", "", fmt.Errorf("failed to update secret: %w", err)
	}

	log.Info("Successfully updated secret",
		"secretArn", aws.ToString(updateOutput.ARN),
		"versionId", aws.ToString(updateOutput.VersionId))

	return aws.ToString(updateOutput.ARN), aws.ToString(updateOutput.VersionId), nil
}

// deleteSecret deletes the secret from AWS Secrets Manager
func deleteSecret(ctx context.Context, secretArn string) error {
	log := logf.FromContext(ctx)

	deleteInput := &secretsmanager.DeleteSecretInput{
		SecretId:                   aws.String(secretArn),
		ForceDeleteWithoutRecovery: aws.Bool(true),
	}

	_, err := smClient.DeleteSecret(ctx, deleteInput)
	if err == nil {
		log.Info("Successfully deleted secret", "secretArn", secretArn)
		return nil
	}

	var notFoundErr *types.ResourceNotFoundException
	if errors.As(err, &notFoundErr) {
		log.Info("Secret already deleted", "secretArn", secretArn)
		return nil
	}

	return fmt.Errorf("failed to delete secret: %w", err)
}

// Generate via AWS GetRandomPassword
func getRandomPassword(ctx context.Context, spec *GeneratorSpec) (string, error) {
	result, err := smClient.GetRandomPassword(ctx, &secretsmanager.GetRandomPasswordInput{
		PasswordLength:          aws.Int64(spec.PasswordLength),
		RequireEachIncludedType: aws.Bool(spec.RequireEachIncludedType),
		ExcludePunctuation:      aws.Bool(spec.ExcludePunctuation),
		ExcludeNumbers:          aws.Bool(spec.ExcludeNumbers),
		ExcludeLowercase:        aws.Bool(spec.ExcludeLowercase),
		ExcludeUppercase:        aws.Bool(spec.ExcludeUppercase),
		IncludeSpace:            aws.Bool(spec.IncludeSpace),
		ExcludeCharacters:       aws.String(spec.ExcludeCharacters),
	})

	if err != nil {
		return "", nil
	}

	return aws.ToString(result.RandomPassword), nil
}

// awsErrorClassifier wraps the AWS SDK retry logic for use with result builder utilities.
var awsErrorClassifier = controller.ErrorClassifier(isRetryable)

// isRetryable determines if an error is retryable using AWS SDK's built-in error classification.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Use AWS SDK's built-in retry classification
	// This handles all AWS API errors, network errors, and HTTP status codes
	for _, checker := range retry.DefaultRetryables {
		if checker.IsErrorRetryable(err) == aws.TrueTernary {
			return true
		}
	}

	return false
}
