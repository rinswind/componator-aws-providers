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
		return controller.ActionSuccess(op.status)
	}

	log.Info("Starting secret deletion")

	// Delete secret (with force to skip recovery window for cleaner deletion)
	deleteInput := &secretsmanager.DeleteSecretInput{
		SecretId:                   aws.String(op.status.SecretArn),
		ForceDeleteWithoutRecovery: aws.Bool(true),
	}

	_, err := op.smClient.DeleteSecret(ctx, deleteInput)
	if err != nil {
		var notFoundErr *types.ResourceNotFoundException
		if errors.As(err, &notFoundErr) {
			log.Info("Secret already deleted", "secretArn", op.status.SecretArn)
			return controller.ActionSuccess(op.status)
		}
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to delete secret: %w", err), awsErrorClassifier)
	}

	log.Info("Successfully initiated secret deletion", "secretArn", op.status.SecretArn)
	return controller.ActionSuccess(op.status)
}

// CheckDeletion verifies deletion is complete
func (op *SecretPushOperations) CheckDeletion(ctx context.Context) (*controller.CheckResult, error) {
	log := logf.FromContext(ctx).WithValues("secretName", op.config.SecretName)

	// If no ARN in status or DeletionPolicy is Retain, deletion is complete
	if op.status.SecretArn == "" || op.config.DeletionPolicy == DeletionPolicyRetain {
		log.V(1).Info("Deletion complete")
		return controller.CheckComplete(op.status)
	}

	// Verify secret no longer exists
	_, err := op.smClient.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
		SecretId: aws.String(op.status.SecretArn),
	})

	if err != nil {
		var notFoundErr *types.ResourceNotFoundException
		if errors.As(err, &notFoundErr) {
			log.Info("Secret deletion verified", "secretArn", op.status.SecretArn)
			return controller.CheckComplete(op.status)
		}
		return nil, fmt.Errorf("failed to check deletion status: %w", err)
	}

	// Secret still exists - deletion in progress
	log.V(1).Info("Secret still exists, deletion in progress", "secretArn", op.status.SecretArn)
	return controller.CheckInProgress(op.status)
}
