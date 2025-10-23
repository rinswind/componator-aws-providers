// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package secretpush

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/rinswind/deployment-operator/componentkit/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Delete implements deletion operations for secrets
func (op *SecretPushOperations) Delete(ctx context.Context) (*controller.ActionResult, error) {
	log := logf.FromContext(ctx).WithValues("secretName", op.config.SecretName, "secretArn", op.status.SecretArn)

	// If no secret ARN in status, nothing to delete
	if op.status.SecretArn == "" {
		log.Info("No secret ARN in status, nothing to delete")
		return controller.ActionSuccess(op.status)
	}

	// Check deletion policy
	if op.config.DeletionPolicy == DeletionPolicyRetain {
		log.Info("DeletionPolicy is Retain, skipping secret deletion", "secretArn", op.status.SecretArn)
		details := fmt.Sprintf("Secret %s retained (Retain policy)", op.config.SecretName)
		return controller.ActionSuccessWithDetails(op.status, details)
	}

	log.Info("Starting secret deletion")

	// Delete secret from AWS
	err := op.deleteSecretInAWS(ctx)
	if err != nil {
		return controller.ActionResultForError(op.status, err, awsErrorClassifier)
	}

	// Status remains unchanged (no update needed for deletion)
	details := fmt.Sprintf("Deleting secret %s", op.config.SecretName)
	return controller.ActionSuccessWithDetails(op.status, details)
}

// deleteSecretInAWS deletes the secret from AWS Secrets Manager
func (op *SecretPushOperations) deleteSecretInAWS(ctx context.Context) error {
	log := logf.FromContext(ctx)

	deleteInput := &secretsmanager.DeleteSecretInput{
		SecretId:                   aws.String(op.status.SecretArn),
		ForceDeleteWithoutRecovery: aws.Bool(true),
	}

	_, err := op.smClient.DeleteSecret(ctx, deleteInput)
	if err != nil {
		var notFoundErr *types.ResourceNotFoundException
		if errors.As(err, &notFoundErr) {
			log.Info("Secret already deleted", "secretArn", op.status.SecretArn)
			return nil
		}
		return fmt.Errorf("failed to delete secret: %w", err)
	}

	log.Info("Successfully initiated secret deletion", "secretArn", op.status.SecretArn)
	return nil
}

// CheckDeletion verifies deletion is complete
func (op *SecretPushOperations) CheckDeletion(ctx context.Context) (*controller.CheckResult, error) {
	log := logf.FromContext(ctx).WithValues("secretName", op.config.SecretName)

	// If no ARN in status or DeletionPolicy is Retain, deletion is complete
	if op.status.SecretArn == "" || op.config.DeletionPolicy == DeletionPolicyRetain {
		log.V(1).Info("Deletion complete")
		return controller.CheckComplete(op.status)
	}

	// Check if secret still exists
	stillExists, err := op.secretStillExists(ctx)
	if err != nil {
		return nil, err
	}

	if stillExists {
		log.V(1).Info("Secret still exists, deletion in progress", "secretArn", op.status.SecretArn)
		details := fmt.Sprintf("Waiting for secret %s deletion", op.config.SecretName)
		return controller.CheckInProgressWithDetails(op.status, details)
	}

	log.Info("Secret deletion verified", "secretArn", op.status.SecretArn)
	details := fmt.Sprintf("Secret %s deleted", op.config.SecretName)
	return controller.CheckCompleteWithDetails(op.status, details)
}

// secretStillExists checks if the secret still exists during deletion
// Returns: exists, error
func (op *SecretPushOperations) secretStillExists(ctx context.Context) (bool, error) {
	_, err := op.smClient.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
		SecretId: aws.String(op.status.SecretArn),
	})

	if err != nil {
		var notFoundErr *types.ResourceNotFoundException
		if errors.As(err, &notFoundErr) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check deletion status: %w", err)
	}

	return true, nil
}
