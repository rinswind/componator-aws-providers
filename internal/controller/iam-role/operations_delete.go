// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package iamrole

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/rinswind/deployment-operator/componentkit/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Delete removes the IAM role after detaching all managed policies.
// This method handles:
// - Listing all attached managed policies
// - Detaching each policy
// - Deleting the role
func (op *IamRoleOperations) Delete(ctx context.Context) (*controller.ActionResult, error) {
	log := logf.FromContext(ctx).WithValues("roleName", op.config.RoleName)

	log.Info("Starting IAM role deletion")

	// Check if role exists - if not, deletion is already complete
	role, err := op.getRoleByName(ctx, op.config.RoleName)
	if err != nil {
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to check if role exists: %w", err), iamErrorClassifier)
	}

	if role == nil {
		log.Info("Role already deleted")
		return controller.ActionSuccess(op.status)
	}

	// List all attached managed policies
	attachedPolicies, err := op.listAttachedPolicies(ctx, op.config.RoleName)
	if err != nil {
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to list attached policies: %w", err), iamErrorClassifier)
	}

	// Detach all managed policies
	if len(attachedPolicies) > 0 {
		log.Info("Detaching managed policies before deletion", "count", len(attachedPolicies))
		for _, policyArn := range attachedPolicies {
			log.V(1).Info("Detaching policy", "policyArn", policyArn)
			if err := op.detachPolicy(ctx, op.config.RoleName, policyArn); err != nil {
				return controller.ActionResultForError(
					op.status, fmt.Errorf("failed to detach policy %s: %w", policyArn, err), iamErrorClassifier)
			}
		}
		log.Info("Successfully detached all policies")
	}

	// Delete the role
	if err := op.deleteRole(ctx, op.config.RoleName); err != nil {
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to delete role: %w", err), iamErrorClassifier)
	}

	log.Info("Successfully deleted IAM role")
	return controller.ActionSuccess(op.status)
}

// CheckDeletion verifies the IAM role has been successfully deleted.
func (op *IamRoleOperations) CheckDeletion(ctx context.Context) (*controller.CheckResult, error) {
	log := logf.FromContext(ctx).WithValues("roleName", op.config.RoleName)

	// Check if role still exists
	role, err := op.getRoleByName(ctx, op.config.RoleName)
	if err != nil {
		return controller.CheckResultForError(
			op.status, fmt.Errorf("failed to check role deletion status: %w", err), iamErrorClassifier)
	}

	if role == nil {
		log.Info("Role deletion confirmed")
		return controller.CheckComplete(op.status)
	}

	log.V(1).Info("Role still exists, deletion in progress")
	return controller.CheckInProgress(op.status)
}

// deleteRole deletes the IAM role
func (op *IamRoleOperations) deleteRole(ctx context.Context, roleName string) error {
	input := &iam.DeleteRoleInput{
		RoleName: aws.String(roleName),
	}

	_, err := op.iamClient.DeleteRole(ctx, input)
	if err != nil {
		// If role already deleted, treat as success
		if isNotFoundError(err) {
			return nil
		}
		return fmt.Errorf("failed to delete role: %w", err)
	}

	return nil
}
