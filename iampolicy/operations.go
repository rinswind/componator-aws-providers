// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package iampolicy

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/rinswind/componator/componentkit/functional"
	k8stypes "k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// applyAction initiates IAM policy creation or update
func applyAction(
	ctx context.Context,
	name k8stypes.NamespacedName,
	spec IamPolicyConfig,
	status IamPolicyStatus) (*functional.ActionResult[IamPolicyStatus], error) {

	// Validate and apply defaults to config
	if err := resolveSpec(&spec); err != nil {
		return functional.ActionFailure(status, fmt.Sprintf("config validation failed: %v", err))
	}

	log := logf.FromContext(ctx).WithValues("policyName", spec.PolicyName)
	log.Info("Starting IAM policy deployment")

	// Check if policy already exists
	existingPolicy, err := getPolicyByName(ctx, spec.PolicyName, spec.Path)
	if err != nil {
		return functional.ActionResultForError(status, fmt.Errorf("failed to check if policy exists: %w", err), iamErrorClassifier)
	}

	if existingPolicy == nil {
		// Policy doesn't exist - create it
		policy, err := createPolicy(ctx, spec.PolicyName, spec.PolicyDocument, spec.Path, spec.Description, spec.Tags)
		if err != nil {
			return functional.ActionResultForError(status, fmt.Errorf("failed to create policy: %w", err), iamErrorClassifier)
		}

		// Update status with created policy info
		status.PolicyArn = aws.ToString(policy.Arn)
		status.PolicyId = aws.ToString(policy.PolicyId)
		status.PolicyName = aws.ToString(policy.PolicyName)
		status.CurrentVersionId = aws.ToString(policy.DefaultVersionId)

		details := fmt.Sprintf("Created policy %s", status.PolicyName)
		return functional.ActionSuccess(status, details)
	}

	// Policy exists - update status and create new version if needed
	status.PolicyArn = aws.ToString(existingPolicy.Arn)
	status.PolicyId = aws.ToString(existingPolicy.PolicyId)
	status.PolicyName = aws.ToString(existingPolicy.PolicyName)

	log.Info("Policy already exists, checking for updates", "policyArn", status.PolicyArn)

	versionId, err := createPolicyVersion(ctx, status.PolicyArn, spec.PolicyDocument)
	if err != nil {
		return functional.ActionResultForError(status, fmt.Errorf("failed to update policy version: %w", err), iamErrorClassifier)
	}

	// Update status with current version
	status.CurrentVersionId = versionId

	details := fmt.Sprintf("Updated policy %s to version %s", status.PolicyName, versionId)
	return functional.ActionSuccess(status, details)
}

// checkApplied verifies policy exists and is ready
func checkApplied(
	ctx context.Context,
	name k8stypes.NamespacedName,
	spec IamPolicyConfig,
	status IamPolicyStatus) (*functional.CheckResult[IamPolicyStatus], error) {

	log := logf.FromContext(ctx).WithValues("policyName", spec.PolicyName)

	// If we don't have a policy ARN yet, deployment hasn't started
	if status.PolicyArn == "" {
		log.V(1).Info("No policy ARN in status, deployment not started")
		return functional.CheckInProgress(status, "")
	}

	// Verify policy exists
	policy, err := getPolicyByArn(ctx, status.PolicyArn)
	if err != nil {
		return functional.CheckResultForError(status, fmt.Errorf("failed to check policy status: %w", err), iamErrorClassifier)
	}

	if policy == nil {
		return functional.CheckResultForError(status,
			fmt.Errorf("policy not found at ARN %s", status.PolicyArn), iamErrorClassifier)
	}

	// Update status with current policy info
	status.PolicyArn = aws.ToString(policy.Arn)
	status.PolicyId = aws.ToString(policy.PolicyId)
	status.PolicyName = aws.ToString(policy.PolicyName)

	details := fmt.Sprintf("Policy %s ready (version %s)", status.PolicyName, status.CurrentVersionId)
	return functional.CheckComplete(status, details)
}

// deleteAction implements deletion operations for IAM policies
func deleteAction(
	ctx context.Context,
	name k8stypes.NamespacedName,
	spec IamPolicyConfig,
	status IamPolicyStatus) (*functional.ActionResult[IamPolicyStatus], error) {

	log := logf.FromContext(ctx).WithValues("policyName", spec.PolicyName, "policyArn", status.PolicyArn)

	// If no policy ARN in status, nothing to delete
	if status.PolicyArn == "" {
		log.Info("No policy ARN in status, nothing to delete")
		return functional.ActionSuccess(status, "No policy to delete")
	}

	log.Info("Starting IAM policy deletion")

	// Verify policy exists before attempting deletion
	policy, err := getPolicyByArn(ctx, status.PolicyArn)
	if err != nil {
		return functional.ActionResultForError(status, fmt.Errorf("failed to check policy existence: %w", err), iamErrorClassifier)
	}

	// If policy doesn't exist, deletion is already complete
	if policy == nil {
		log.Info("Policy already deleted", "policyArn", status.PolicyArn)
		return functional.ActionSuccess(status, "Policy already deleted")
	}

	// Delete all non-default versions first
	if err := deletePolicyAllVersions(ctx, status.PolicyArn); err != nil {
		return functional.ActionResultForError(status, fmt.Errorf("failed to delete policy versions: %w", err), iamErrorClassifier)
	}

	// Delete the policy itself
	if err := deletePolicy(ctx, status.PolicyArn); err != nil {
		return functional.ActionResultForError(status, fmt.Errorf("failed to delete policy: %w", err), iamErrorClassifier)
	}

	details := fmt.Sprintf("Deleting policy %s", status.PolicyName)
	return functional.ActionSuccess(status, details)
}

// checkDeleted verifies deletion is complete
func checkDeleted(
	ctx context.Context,
	name k8stypes.NamespacedName,
	spec IamPolicyConfig,
	status IamPolicyStatus) (*functional.CheckResult[IamPolicyStatus], error) {

	log := logf.FromContext(ctx).WithValues("policyName", spec.PolicyName)

	// If no policy ARN in status, deletion is complete
	if status.PolicyArn == "" {
		log.V(1).Info("No policy ARN in status, deletion complete")
		return functional.CheckComplete(status, "")
	}

	// Verify policy no longer exists
	policy, err := getPolicyByArn(ctx, status.PolicyArn)
	if err != nil {
		return functional.CheckResultForError(status, fmt.Errorf("failed to check policy deletion status: %w", err), iamErrorClassifier)
	}

	// Policy still exists - deletion in progress
	if policy != nil {
		log.V(1).Info("Policy still exists, deletion in progress", "policyArn", status.PolicyArn)
		details := fmt.Sprintf("Waiting for policy %s deletion", status.PolicyName)
		return functional.CheckInProgress(status, details)
	}

	details := fmt.Sprintf("Policy %s deleted", status.PolicyName)
	return functional.CheckComplete(status, details)
}
