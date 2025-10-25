// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package iamrole

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/rinswind/componator/componentkit/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Deploy creates or updates the IAM role with trust policy and managed policy attachments.
// This method handles:
// - Creating new roles with trust policy
// - Updating trust policy on existing roles
// - Reconciling managed policy attachments (add/remove to match desired state)
func (op *IamRoleOperations) Deploy(ctx context.Context) (*controller.ActionResult, error) {
	log := logf.FromContext(ctx).WithValues("roleName", op.config.RoleName)

	log.Info("Starting IAM role deployment")

	// Check if role already exists
	existingRole, err := op.getRoleByName(ctx, op.config.RoleName)
	if err != nil {
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to check if role exists: %w", err), iamErrorClassifier)
	}

	if existingRole == nil {
		// Role doesn't exist - create it
		role, err := op.createRole(
			ctx,
			op.config.RoleName,
			op.config.AssumeRolePolicy,
			op.config.Path,
			op.config.Description,
			op.config.MaxSessionDuration,
			op.config.Tags)
		if err != nil {
			return controller.ActionResultForError(
				op.status, fmt.Errorf("failed to create role: %w", err), iamErrorClassifier)
		}

		// Update status with created role info
		op.status.RoleArn = aws.ToString(role.Arn)
		op.status.RoleId = aws.ToString(role.RoleId)
		op.status.RoleName = aws.ToString(role.RoleName)

		// Attach all managed policies
		log.Info("Attaching managed policies to new role", "count", len(op.config.ManagedPolicyArns))
		result, err := op.reconcilePolicyAttachments(ctx, op.config.RoleName, op.config.ManagedPolicyArns)

		// Always update status with actual attached policies (even on partial failure)
		if result != nil {
			op.status.AttachedPolicies = result.AttachedPolicies
		}

		if err != nil {
			return controller.ActionResultForError(
				op.status, fmt.Errorf("failed to attach policies: %w", err), iamErrorClassifier)
		}

		log.Info("Successfully deployed new role", "roleArn", op.status.RoleArn)
		details := fmt.Sprintf("Created role %s with %d policies", op.status.RoleName, len(result.AttachedPolicies))
		return controller.ActionSuccessWithDetails(op.status, details)
	}

	// Role exists - update status and reconcile configuration
	op.status.RoleArn = aws.ToString(existingRole.Arn)
	op.status.RoleId = aws.ToString(existingRole.RoleId)
	op.status.RoleName = aws.ToString(existingRole.RoleName)

	log.Info("Role already exists, reconciling configuration", "roleArn", op.status.RoleArn)

	// Update trust policy if changed
	currentPolicy := aws.ToString(existingRole.AssumeRolePolicyDocument)
	if err := op.updateTrustPolicy(ctx, op.config.RoleName, currentPolicy, op.config.AssumeRolePolicy); err != nil {
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to update trust policy: %w", err), iamErrorClassifier)
	}

	// Reconcile policy attachments
	result, err := op.reconcilePolicyAttachments(ctx, op.config.RoleName, op.config.ManagedPolicyArns)

	// Always update status with actual attached policies (even on partial failure)
	if result != nil {
		op.status.AttachedPolicies = result.AttachedPolicies
	}

	if err != nil {
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to reconcile policy attachments: %w", err), iamErrorClassifier)
	}

	log.Info("Successfully reconciled role", "roleArn", op.status.RoleArn)

	// Build detailed message about changes
	var details string
	if result.AttachedCount > 0 || result.DetachedCount > 0 {
		details = fmt.Sprintf("Updated role %s: attached %d, detached %d, total %d policies",
			op.status.RoleName, result.AttachedCount, result.DetachedCount, len(result.AttachedPolicies))
	} else {
		details = fmt.Sprintf("Role %s unchanged with %d policies", op.status.RoleName, len(result.AttachedPolicies))
	}
	return controller.ActionSuccessWithDetails(op.status, details)
}

// CheckDeployment verifies if the IAM role exists and is in the desired state.
// Returns check result with updated handler status.
func (op *IamRoleOperations) CheckDeployment(ctx context.Context) (*controller.CheckResult, error) {
	log := logf.FromContext(ctx).WithValues("roleName", op.config.RoleName)

	// If we don't have a role ARN yet, deployment hasn't started
	if op.status.RoleArn == "" {
		log.V(1).Info("No role ARN in status, deployment not started")
		return controller.CheckInProgress(op.status)
	}

	// Verify role exists
	role, err := op.getRoleByName(ctx, op.config.RoleName)
	if err != nil {
		return nil, fmt.Errorf("failed to check role status: %w", err)
	}

	if role == nil {
		return controller.CheckResultForError(
			op.status, fmt.Errorf("role not found: %s", op.config.RoleName), iamErrorClassifier)
	}

	// Update status with current role info
	op.status.RoleArn = aws.ToString(role.Arn)
	op.status.RoleId = aws.ToString(role.RoleId)
	op.status.RoleName = aws.ToString(role.RoleName)

	log.Info("Role deployment complete", "roleArn", op.status.RoleArn, "roleId", op.status.RoleId)

	details := fmt.Sprintf("Role %s ready with %d policies", op.status.RoleName, len(op.status.AttachedPolicies))
	return controller.CheckCompleteWithDetails(op.status, details)
}

// jsonEquals compares two policy documents for semantic equality.
// Returns false if either document is invalid JSON.
// Uses reflect.DeepEqual after unmarshaling to handle whitespace and key ordering differences.
func jsonEquals(a, b string) bool {
	// Parse both JSON documents into interface{} for semantic comparison
	var objA, objB interface{}

	if err := json.Unmarshal([]byte(a), &objA); err != nil {
		// Invalid JSON in a - treat as not equal to trigger update
		return false
	}

	if err := json.Unmarshal([]byte(b), &objB); err != nil {
		// Invalid JSON in b - treat as not equal to trigger update
		return false
	}

	// Compare semantically using reflect.DeepEqual
	return reflect.DeepEqual(objA, objB)
}

// createRole creates a new IAM role with the specified trust policy and returns the created role
func (op *IamRoleOperations) createRole(ctx context.Context,
	roleName, assumeRolePolicy, path, description string,
	maxSessionDuration int32,
	tags map[string]string) (*types.Role, error) {

	log := logf.FromContext(ctx).WithValues("roleName", roleName)

	log.Info("Creating new IAM role")

	input := &iam.CreateRoleInput{
		RoleName:                 aws.String(roleName),
		AssumeRolePolicyDocument: aws.String(assumeRolePolicy),
		Path:                     aws.String(path),
		MaxSessionDuration:       aws.Int32(maxSessionDuration),
		Description:              aws.String(description),
		Tags:                     toIAMTags(tags),
	}

	output, err := op.iamClient.CreateRole(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to create role: %w", err)
	}

	log.Info("Successfully created IAM role", "roleArn", aws.ToString(output.Role.Arn))

	return output.Role, nil
}

// toIAMTags converts map to IAM tag slice
func toIAMTags(tags map[string]string) []types.Tag {
	if len(tags) == 0 {
		return nil
	}

	result := make([]types.Tag, 0, len(tags))
	for k, v := range tags {
		result = append(result, types.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	return result
}

// updateTrustPolicy updates the assume role policy document for an existing role
func (op *IamRoleOperations) updateTrustPolicy(ctx context.Context, roleName, currentPolicy, desiredPolicy string) error {
	log := logf.FromContext(ctx).WithValues("roleName", roleName)

	// Compare policies (URL-decoded JSON from AWS vs our config)
	if jsonEquals(currentPolicy, desiredPolicy) {
		log.V(1).Info("Trust policy unchanged, skipping update")
		return nil
	}

	log.Info("Trust policy changed, updating")

	input := &iam.UpdateAssumeRolePolicyInput{
		RoleName:       aws.String(roleName),
		PolicyDocument: aws.String(desiredPolicy),
	}

	_, err := op.iamClient.UpdateAssumeRolePolicy(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to update assume role policy: %w", err)
	}

	log.Info("Successfully updated trust policy")
	return nil
}

// PolicyReconciliationResult contains the outcome of policy attachment reconciliation
type PolicyReconciliationResult struct {
	AttachedPolicies []string // Final list of attached policies
	AttachedCount    int      // Number of policies attached during this operation
	DetachedCount    int      // Number of policies detached during this operation
}

// reconcilePolicyAttachments ensures the role has exactly the desired managed policies attached.
// Returns the actual list of attached policies after reconciliation (which may be partial on failure).
// This allows status to reflect reality even when reconciliation fails partway through.
func (op *IamRoleOperations) reconcilePolicyAttachments(ctx context.Context, roleName string, desiredPolicies []string) (*PolicyReconciliationResult, error) {
	log := logf.FromContext(ctx).WithValues("roleName", roleName)

	// Get currently attached policies
	currentPolicies, err := op.listAttachedPolicies(ctx, roleName)
	if err != nil {
		return nil, err
	}

	// Convert to sets for comparison
	desiredSet := make(map[string]bool)
	for _, arn := range desiredPolicies {
		desiredSet[arn] = true
	}

	currentSet := make(map[string]bool)
	for _, arn := range currentPolicies {
		currentSet[arn] = true
	}

	// Compute policies that need to be attached
	toAttachSet := maps.Clone(desiredSet)
	maps.DeleteFunc(toAttachSet, func(k string, _ bool) bool {
		return currentSet[k] // Delete if exists in current
	})
	toAttach := slices.Sorted(maps.Keys(toAttachSet))

	// Compute policies that need to be detached
	toDetachSet := maps.Clone(currentSet)
	maps.DeleteFunc(toDetachSet, func(k string, _ bool) bool {
		return desiredSet[k] // Delete if exists in desired
	})
	toDetach := slices.Sorted(maps.Keys(toDetachSet))

	// Track actual state - starts with current, updated as we make changes
	actuallyAttached := make(map[string]bool)
	for _, arn := range currentPolicies {
		actuallyAttached[arn] = true
	}

	// Check whether we have any changes to make
	if len(toAttach) == 0 && len(toDetach) == 0 {
		log.V(1).Info("Policy attachments already in desired state")
		return &PolicyReconciliationResult{
			AttachedPolicies: slices.Sorted(maps.Keys(actuallyAttached)),
			AttachedCount:    0,
			DetachedCount:    0,
		}, nil
	}

	// Detach removed policies first (cleanup before adding)
	for _, arn := range toDetach {
		log.Info("Detaching policy", "policyArn", arn)
		if err := op.detachPolicy(ctx, roleName, arn); err != nil {
			// Return current actual state even on failure
			return &PolicyReconciliationResult{
				AttachedPolicies: slices.Sorted(maps.Keys(actuallyAttached)),
				AttachedCount:    0,
				DetachedCount:    len(toDetach) - len(actuallyAttached) + len(currentPolicies),
			}, fmt.Errorf("failed to detach policy %s: %w", arn, err)
		}
		delete(actuallyAttached, arn)
	}

	// Attach missing policies
	attachedInThisOp := 0
	for _, arn := range toAttach {
		log.Info("Attaching policy", "policyArn", arn)
		if err := op.attachPolicy(ctx, roleName, arn); err != nil {
			// Return partial progress - what we actually have attached
			return &PolicyReconciliationResult{
				AttachedPolicies: slices.Sorted(maps.Keys(actuallyAttached)),
				AttachedCount:    attachedInThisOp,
				DetachedCount:    len(toDetach),
			}, fmt.Errorf("failed to attach policy %s: %w", arn, err)
		}
		actuallyAttached[arn] = true
		attachedInThisOp++
	}

	log.Info("Policy attachments reconciled", "attached", len(toAttach), "detached", len(toDetach))

	return &PolicyReconciliationResult{
		AttachedPolicies: slices.Sorted(maps.Keys(actuallyAttached)),
		AttachedCount:    len(toAttach),
		DetachedCount:    len(toDetach),
	}, nil
}
