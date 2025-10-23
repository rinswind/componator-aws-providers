// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package iampolicy

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/rinswind/deployment-operator/componentkit/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Delete implements deletion operations for IAM policies
func (op *IamPolicyOperations) Delete(ctx context.Context) (*controller.ActionResult, error) {
	log := logf.FromContext(ctx).WithValues("policyName", op.config.PolicyName, "policyArn", op.status.PolicyArn)

	// If no policy ARN in status, nothing to delete
	if op.status.PolicyArn == "" {
		log.Info("No policy ARN in status, nothing to delete")
		return controller.ActionSuccess(op.status)
	}

	log.Info("Starting IAM policy deletion")

	// Verify policy exists before attempting deletion
	policy, err := op.getPolicyByArn(ctx, op.status.PolicyArn)
	if err != nil {
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to check policy existence: %w", err), iamErrorClassifier)
	}

	// If policy doesn't exist, deletion is already complete
	if policy == nil {
		log.Info("Policy already deleted", "policyArn", op.status.PolicyArn)
		return controller.ActionSuccess(op.status)
	}

	// Delete all non-default versions first
	if err := op.deletePolicyAllVersions(ctx, op.status.PolicyArn); err != nil {
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to delete policy versions: %w", err), iamErrorClassifier)
	}

	// Delete the policy itself
	if err := op.deletePolicy(ctx, op.status.PolicyArn); err != nil {
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to delete policy: %w", err), iamErrorClassifier)
	}

	log.Info("Successfully initiated policy deletion", "policyArn", op.status.PolicyArn)
	details := fmt.Sprintf("Deleting policy %s", op.status.PolicyName)
	return controller.ActionSuccessWithDetails(op.status, details)
}

// CheckDeletion verifies deletion is complete
func (op *IamPolicyOperations) CheckDeletion(ctx context.Context) (*controller.CheckResult, error) {
	log := logf.FromContext(ctx).WithValues("policyName", op.config.PolicyName)

	// If no policy ARN in status, deletion is complete
	if op.status.PolicyArn == "" {
		log.V(1).Info("No policy ARN in status, deletion complete")
		return controller.CheckComplete(op.status)
	}

	// Verify policy no longer exists
	policy, err := op.getPolicyByArn(ctx, op.status.PolicyArn)
	if err != nil {
		return nil, fmt.Errorf("failed to check policy deletion status: %w", err)
	}

	// Policy still exists - deletion in progress
	if policy != nil {
		log.V(1).Info("Policy still exists, deletion in progress", "policyArn", op.status.PolicyArn)
		details := fmt.Sprintf("Waiting for policy %s deletion", op.status.PolicyName)
		return controller.CheckInProgressWithDetails(op.status, details)
	}

	log.Info("Policy deletion verified", "policyArn", op.status.PolicyArn)

	details := fmt.Sprintf("Policy %s deleted", op.status.PolicyName)
	return controller.CheckCompleteWithDetails(op.status, details)
}

// deletePolicy deletes an IAM policy by ARN
func (op *IamPolicyOperations) deletePolicy(ctx context.Context, policyArn string) error {
	log := logf.FromContext(ctx).WithValues("policyArn", policyArn)

	deleteInput := &iam.DeletePolicyInput{
		PolicyArn: aws.String(policyArn),
	}

	_, err := op.iamClient.DeletePolicy(ctx, deleteInput)
	if err == nil {
		log.Info("Successfully deleted IAM policy", "policyArn", policyArn)
		return nil
	}

	// If policy not found, deletion already complete
	if isNotFoundError(err) {
		log.Info("Policy not found during deletion, already deleted", "policyArn", policyArn)
		return nil
	}

	return fmt.Errorf("failed to delete policy: %w", err)
}

// deletePolicyAllVersions deletes all non-default versions of a policy
func (op *IamPolicyOperations) deletePolicyAllVersions(ctx context.Context, policyArn string) error {
	log := logf.FromContext(ctx).WithValues("policyArn", policyArn)

	// List all versions
	listInput := &iam.ListPolicyVersionsInput{
		PolicyArn: aws.String(policyArn),
	}

	listOutput, err := op.iamClient.ListPolicyVersions(ctx, listInput)
	if err != nil {
		// If policy not found, versions already gone
		if isNotFoundError(err) {
			log.V(1).Info("Policy not found when listing versions, already deleted")
			return nil
		}
		return fmt.Errorf("failed to list policy versions: %w", err)
	}

	// Delete all non-default versions
	var deletedCount int
	for i := range listOutput.Versions {
		version := &listOutput.Versions[i]

		// Skip default version (will be deleted with policy)
		if version.IsDefaultVersion {
			continue
		}

		deleteInput := &iam.DeletePolicyVersionInput{
			PolicyArn: aws.String(policyArn),
			VersionId: version.VersionId,
		}

		_, err := op.iamClient.DeletePolicyVersion(ctx, deleteInput)
		if err != nil {
			// If version not found, it's already deleted - continue
			if isNotFoundError(err) {
				log.V(1).Info("Version already deleted", "versionId", aws.ToString(version.VersionId))
				continue
			}
			return fmt.Errorf("failed to delete policy version %s: %w", aws.ToString(version.VersionId), err)
		}

		deletedCount++
		log.V(1).Info("Deleted policy version", "versionId", aws.ToString(version.VersionId))
	}

	if deletedCount > 0 {
		log.Info("Deleted non-default policy versions", "count", deletedCount)
	}

	return nil
}
