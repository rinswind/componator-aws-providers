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
	"github.com/rinswind/deployment-operator/componentkit/controller"
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
	existingRole, err := op.getRoleByName(ctx)
	if err != nil {
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to check if role exists: %w", err), iamErrorClassifier)
	}

	if existingRole == nil {
		// Role doesn't exist - create it
		role, err := op.createRole(ctx)
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
		attachedPolicies, err := op.reconcilePolicyAttachments(ctx)

		// Always update status with actual attached policies (even on partial failure)
		op.status.AttachedPolicies = attachedPolicies

		if err != nil {
			return controller.ActionResultForError(
				op.status, fmt.Errorf("failed to attach policies: %w", err), iamErrorClassifier)
		}

		log.Info("Successfully deployed new role", "roleArn", op.status.RoleArn)
		return controller.ActionSuccess(op.status)
	}

	// Role exists - update status and reconcile configuration
	op.status.RoleArn = aws.ToString(existingRole.Arn)
	op.status.RoleId = aws.ToString(existingRole.RoleId)
	op.status.RoleName = aws.ToString(existingRole.RoleName)

	log.Info("Role already exists, reconciling configuration", "roleArn", op.status.RoleArn)

	// Update trust policy if changed
	if err := op.updateTrustPolicy(ctx); err != nil {
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to update trust policy: %w", err), iamErrorClassifier)
	}

	// Reconcile policy attachments
	attachedPolicies, err := op.reconcilePolicyAttachments(ctx)

	// Always update status with actual attached policies (even on partial failure)
	op.status.AttachedPolicies = attachedPolicies

	if err != nil {
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to reconcile policy attachments: %w", err), iamErrorClassifier)
	}

	log.Info("Successfully reconciled role", "roleArn", op.status.RoleArn)
	return controller.ActionSuccess(op.status)
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
	role, err := op.getRoleByName(ctx)
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

	return controller.CheckComplete(op.status)
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
func (op *IamRoleOperations) createRole(ctx context.Context) (*types.Role, error) {
	log := logf.FromContext(ctx).WithValues("roleName", op.config.RoleName)

	log.Info("Creating new IAM role")

	input := &iam.CreateRoleInput{
		RoleName:                 aws.String(op.config.RoleName),
		AssumeRolePolicyDocument: aws.String(op.config.AssumeRolePolicy),
		Path:                     aws.String(op.config.Path),
		MaxSessionDuration:       aws.Int32(op.config.MaxSessionDuration),
	}

	if op.config.Description != "" {
		input.Description = aws.String(op.config.Description)
	}

	if len(op.config.Tags) > 0 {
		input.Tags = toIAMTags(op.config.Tags)
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
func (op *IamRoleOperations) updateTrustPolicy(ctx context.Context) error {
	log := logf.FromContext(ctx).WithValues("roleName", op.config.RoleName)

	// Get current trust policy
	role, err := op.getRoleByName(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current role: %w", err)
	}
	if role == nil {
		return fmt.Errorf("role not found")
	}

	currentPolicy := aws.ToString(role.AssumeRolePolicyDocument)

	// Compare policies (URL-decoded JSON from AWS vs our config)
	if jsonEquals(currentPolicy, op.config.AssumeRolePolicy) {
		log.V(1).Info("Trust policy unchanged, skipping update")
		return nil
	}

	log.Info("Trust policy changed, updating")

	input := &iam.UpdateAssumeRolePolicyInput{
		RoleName:       aws.String(op.config.RoleName),
		PolicyDocument: aws.String(op.config.AssumeRolePolicy),
	}

	_, err = op.iamClient.UpdateAssumeRolePolicy(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to update assume role policy: %w", err)
	}

	log.Info("Successfully updated trust policy")
	return nil
}

// reconcilePolicyAttachments ensures the role has exactly the desired managed policies attached.
// Returns the actual list of attached policies after reconciliation (which may be partial on failure).
// This allows status to reflect reality even when reconciliation fails partway through.
func (op *IamRoleOperations) reconcilePolicyAttachments(ctx context.Context) ([]string, error) {
	log := logf.FromContext(ctx).WithValues("roleName", op.config.RoleName)

	// Get currently attached policies
	currentPolicies, err := op.listAttachedPolicies(ctx)
	if err != nil {
		return nil, err
	}

	// Convert to sets for comparison
	desiredSet := make(map[string]bool)
	for _, arn := range op.config.ManagedPolicyArns {
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
		return slices.Sorted(maps.Keys(actuallyAttached)), nil
	}

	// Detach removed policies first (cleanup before adding)
	for _, arn := range toDetach {
		log.Info("Detaching policy", "policyArn", arn)
		if err := op.detachPolicy(ctx, arn); err != nil {
			// Return current actual state even on failure
			return slices.Sorted(maps.Keys(actuallyAttached)), fmt.Errorf("failed to detach policy %s: %w", arn, err)
		}
		delete(actuallyAttached, arn)
	}

	// Attach missing policies
	for _, arn := range toAttach {
		log.Info("Attaching policy", "policyArn", arn)
		if err := op.attachPolicy(ctx, arn); err != nil {
			// Return partial progress - what we actually have attached
			return slices.Sorted(maps.Keys(actuallyAttached)), fmt.Errorf("failed to attach policy %s: %w", arn, err)
		}
		actuallyAttached[arn] = true
	}

	log.Info("Policy attachments reconciled", "attached", len(toAttach), "detached", len(toDetach))

	return slices.Sorted(maps.Keys(actuallyAttached)), nil
}
