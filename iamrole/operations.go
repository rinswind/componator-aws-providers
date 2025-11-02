// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package iamrole

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/rinswind/componator/componentkit/controller"
	k8stypes "k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// applyAction creates or updates the IAM role with trust policy and managed policy attachments
func applyAction(
	ctx context.Context,
	name k8stypes.NamespacedName,
	spec IamRoleConfig,
	status IamRoleStatus) (*controller.ActionResultFunc[IamRoleStatus], error) {

	// Validate and apply defaults to config
	if err := resolveSpec(&spec); err != nil {
		return controller.ActionFailureFunc(status, fmt.Sprintf("config validation failed: %v", err))
	}

	log := logf.FromContext(ctx).WithValues("roleName", spec.RoleName)
	log.Info("Starting IAM role deployment")

	// Check if role already exists
	existingRole, err := getRoleByName(ctx, spec.RoleName)
	if err != nil {
		return controller.ActionResultForErrorFunc(status, fmt.Errorf("failed to check if role exists: %w", err), iamErrorClassifier)
	}

	if existingRole == nil {
		// Role doesn't exist - create it
		role, err := createRole(ctx, spec.RoleName, spec.AssumeRolePolicy, spec.Path, spec.Description, spec.MaxSessionDuration, spec.Tags)
		if err != nil {
			return controller.ActionResultForErrorFunc(status, fmt.Errorf("failed to create role: %w", err), iamErrorClassifier)
		}

		// Update status with created role info
		status.RoleArn = aws.ToString(role.Arn)
		status.RoleId = aws.ToString(role.RoleId)
		status.RoleName = aws.ToString(role.RoleName)

		// Attach all managed policies
		log.Info("Attaching managed policies to new role", "count", len(spec.ManagedPolicyArns))
		result, err := reconcilePolicyAttachments(ctx, spec.RoleName, spec.ManagedPolicyArns)

		// Always update status with actual attached policies (even on partial failure)
		if result != nil {
			status.AttachedPolicies = result.AttachedPolicies
		}

		if err != nil {
			return controller.ActionResultForErrorFunc(status, fmt.Errorf("failed to attach policies: %w", err), iamErrorClassifier)
		}

		details := fmt.Sprintf("Created role %s with %d policies", status.RoleName, len(result.AttachedPolicies))
		return controller.ActionSuccessFunc(status, details)
	}

	// Role exists - update status and reconcile configuration
	status.RoleArn = aws.ToString(existingRole.Arn)
	status.RoleId = aws.ToString(existingRole.RoleId)
	status.RoleName = aws.ToString(existingRole.RoleName)

	log.Info("Role already exists, reconciling configuration", "roleArn", status.RoleArn)

	// Update trust policy if changed
	currentPolicy := aws.ToString(existingRole.AssumeRolePolicyDocument)
	if err := updateTrustPolicy(ctx, spec.RoleName, currentPolicy, spec.AssumeRolePolicy); err != nil {
		return controller.ActionResultForErrorFunc(status, fmt.Errorf("failed to update trust policy: %w", err), iamErrorClassifier)
	}

	// Reconcile policy attachments
	result, err := reconcilePolicyAttachments(ctx, spec.RoleName, spec.ManagedPolicyArns)

	// Always update status with actual attached policies (even on partial failure)
	if result != nil {
		status.AttachedPolicies = result.AttachedPolicies
	}

	if err != nil {
		return controller.ActionResultForErrorFunc(status, fmt.Errorf("failed to reconcile policy attachments: %w", err), iamErrorClassifier)
	}

	// Build detailed message about changes
	var details string
	if result.AttachedCount > 0 || result.DetachedCount > 0 {
		details = fmt.Sprintf("Updated role %s: attached %d, detached %d, total %d policies",
			status.RoleName, result.AttachedCount, result.DetachedCount, len(result.AttachedPolicies))
	} else {
		details = fmt.Sprintf("Role %s unchanged with %d policies", status.RoleName, len(result.AttachedPolicies))
	}
	return controller.ActionSuccessFunc(status, details)
}

// checkApplied verifies role exists and is ready
func checkApplied(
	ctx context.Context,
	name k8stypes.NamespacedName,
	spec IamRoleConfig,
	status IamRoleStatus) (*controller.CheckResultFunc[IamRoleStatus], error) {

	log := logf.FromContext(ctx).WithValues("roleName", spec.RoleName)

	// If we don't have a role ARN yet, deployment hasn't started
	if status.RoleArn == "" {
		log.V(1).Info("No role ARN in status, deployment not started")
		return controller.CheckInProgressFunc(status, "")
	}

	// Verify role exists
	role, err := getRoleByName(ctx, spec.RoleName)
	if err != nil {
		return controller.CheckResultForErrorFunc(status, fmt.Errorf("failed to check role status: %w", err), iamErrorClassifier)
	}

	if role == nil {
		return controller.CheckResultForErrorFunc(status,
			fmt.Errorf("role not found: %s", spec.RoleName), iamErrorClassifier)
	}

	// Update status with current role info
	status.RoleArn = aws.ToString(role.Arn)
	status.RoleId = aws.ToString(role.RoleId)
	status.RoleName = aws.ToString(role.RoleName)

	details := fmt.Sprintf("Role %s ready with %d policies", status.RoleName, len(status.AttachedPolicies))
	return controller.CheckCompleteFunc(status, details)
}

// deleteAction removes the IAM role after detaching all managed policies
func deleteAction(
	ctx context.Context,
	name k8stypes.NamespacedName,
	spec IamRoleConfig,
	status IamRoleStatus) (*controller.ActionResultFunc[IamRoleStatus], error) {

	log := logf.FromContext(ctx).WithValues("roleName", spec.RoleName)
	log.Info("Starting IAM role deletion")

	// Check if role exists - if not, deletion is already complete
	role, err := getRoleByName(ctx, spec.RoleName)
	if err != nil {
		return controller.ActionResultForErrorFunc(status, fmt.Errorf("failed to check if role exists: %w", err), iamErrorClassifier)
	}

	if role == nil {
		log.Info("Role already deleted")
		return controller.ActionSuccessFunc(status, "Role already deleted")
	}

	// List all attached managed policies
	attachedPolicies, err := listAttachedPolicies(ctx, spec.RoleName)
	if err != nil {
		return controller.ActionResultForErrorFunc(status, fmt.Errorf("failed to list attached policies: %w", err), iamErrorClassifier)
	}

	detachedCount := 0
	// Detach all managed policies
	if len(attachedPolicies) > 0 {
		log.Info("Detaching managed policies before deletion", "count", len(attachedPolicies))
		for _, policyArn := range attachedPolicies {
			log.V(1).Info("Detaching policy", "policyArn", policyArn)
			if err := detachPolicy(ctx, spec.RoleName, policyArn); err != nil {
				return controller.ActionResultForErrorFunc(status, fmt.Errorf("failed to detach policy %s: %w", policyArn, err), iamErrorClassifier)
			}
			detachedCount++
		}
		log.Info("Successfully detached all policies")
	}

	// Delete the role
	if err := deleteRole(ctx, spec.RoleName); err != nil {
		return controller.ActionResultForErrorFunc(status, fmt.Errorf("failed to delete role: %w", err), iamErrorClassifier)
	}

	details := fmt.Sprintf("Deleting role %s (detached %d policies)", spec.RoleName, detachedCount)
	return controller.ActionSuccessFunc(status, details)
}

// checkDeleted verifies deletion is complete
func checkDeleted(
	ctx context.Context,
	name k8stypes.NamespacedName,
	spec IamRoleConfig,
	status IamRoleStatus) (*controller.CheckResultFunc[IamRoleStatus], error) {

	log := logf.FromContext(ctx).WithValues("roleName", spec.RoleName)

	// Check if role still exists
	role, err := getRoleByName(ctx, spec.RoleName)
	if err != nil {
		return controller.CheckResultForErrorFunc(status, fmt.Errorf("failed to check role deletion status: %w", err), iamErrorClassifier)
	}

	if role == nil {
		log.Info("Role deletion confirmed")
		details := fmt.Sprintf("Role %s deleted", spec.RoleName)
		return controller.CheckCompleteFunc(status, details)
	}

	log.V(1).Info("Role still exists, deletion in progress")
	details := fmt.Sprintf("Waiting for role %s deletion", spec.RoleName)
	return controller.CheckInProgressFunc(status, details)
}
