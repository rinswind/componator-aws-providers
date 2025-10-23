// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package iampolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/rinswind/deployment-operator/componentkit/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	MaxNumberOfPolicyVersions = 5
)

// Deploy initiates IAM policy creation or update
func (op *IamPolicyOperations) Deploy(ctx context.Context) (*controller.ActionResult, error) {
	log := logf.FromContext(ctx).WithValues("policyName", op.config.PolicyName)

	log.Info("Starting IAM policy deployment")

	// Check if policy already exists
	existingPolicy, err := op.getPolicyByName(ctx, op.config.PolicyName, op.config.Path)
	if err != nil {
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to check if policy exists: %w", err), iamErrorClassifier)
	}

	if existingPolicy == nil {
		// Policy doesn't exist - create it
		policy, err := op.createPolicy(ctx,
			op.config.PolicyName,
			op.config.PolicyDocument,
			op.config.Path,
			op.config.Description,
			op.config.Tags)
		if err != nil {
			return controller.ActionResultForError(
				op.status, fmt.Errorf("failed to create policy: %w", err), iamErrorClassifier)
		}

		// Update status with created policy info
		op.status.PolicyArn = aws.ToString(policy.Arn)
		op.status.PolicyId = aws.ToString(policy.PolicyId)
		op.status.PolicyName = aws.ToString(policy.PolicyName)
		op.status.CurrentVersionId = aws.ToString(policy.DefaultVersionId)

		log.Info("Successfully deployed new policy", "policyArn", op.status.PolicyArn)
		details := fmt.Sprintf("Created policy %s", op.status.PolicyName)
		return controller.ActionSuccessWithDetails(op.status, details)
	}

	// Policy exists - update status and create new version if needed
	op.status.PolicyArn = aws.ToString(existingPolicy.Arn)
	op.status.PolicyId = aws.ToString(existingPolicy.PolicyId)
	op.status.PolicyName = aws.ToString(existingPolicy.PolicyName)

	log.Info("Policy already exists, checking for updates", "policyArn", op.status.PolicyArn)

	versionId, err := op.createPolicyVersion(ctx, op.status.PolicyArn, op.config.PolicyDocument)
	if err != nil {
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to update policy version: %w", err), iamErrorClassifier)
	}

	// Update status with current version
	op.status.CurrentVersionId = versionId

	log.Info("Successfully reconciled policy", "policyArn", op.status.PolicyArn, "versionId", versionId)
	details := fmt.Sprintf("Updated policy %s to version %s", op.status.PolicyName, versionId)
	return controller.ActionSuccessWithDetails(op.status, details)
}

// CheckDeployment verifies policy exists and is ready
func (op *IamPolicyOperations) CheckDeployment(ctx context.Context) (*controller.CheckResult, error) {
	log := logf.FromContext(ctx).WithValues("policyName", op.config.PolicyName)

	// If we don't have a policy ARN yet, deployment hasn't started
	if op.status.PolicyArn == "" {
		log.V(1).Info("No policy ARN in status, deployment not started")
		return controller.CheckInProgress(op.status)
	}

	// Verify policy exists
	policy, err := op.getPolicyByArn(ctx, op.status.PolicyArn)
	if err != nil {
		return nil, fmt.Errorf("failed to check policy status: %w", err)
	}

	if policy == nil {
		return controller.CheckResultForError(op.status,
			fmt.Errorf("policy not found at ARN %s", op.status.PolicyArn), iamErrorClassifier)
	}

	// Update status with current policy info
	op.status.PolicyArn = aws.ToString(policy.Arn)
	op.status.PolicyId = aws.ToString(policy.PolicyId)
	op.status.PolicyName = aws.ToString(policy.PolicyName)

	log.Info("Policy deployment complete",
		"policyArn", op.status.PolicyArn,
		"policyId", op.status.PolicyId)

	details := fmt.Sprintf("Policy %s ready (version %s)", op.status.PolicyName, op.status.CurrentVersionId)
	return controller.CheckCompleteWithDetails(op.status, details)
}

// createPolicy creates a new IAM policy and returns the created policy
func (op *IamPolicyOperations) createPolicy(
	ctx context.Context, policyName, policyDocument, path, description string, tags map[string]string) (*types.Policy, error) {

	log := logf.FromContext(ctx).WithValues("policyName", policyName)

	log.Info("Creating new IAM policy")

	input := &iam.CreatePolicyInput{
		PolicyName:     aws.String(policyName),
		PolicyDocument: aws.String(policyDocument),
		Path:           aws.String(path),
		Description:    aws.String(description),
		Tags:           toIAMTags(tags),
	}

	output, err := op.iamClient.CreatePolicy(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to create policy: %w", err)
	}

	log.Info("Successfully created IAM policy",
		"policyArn", aws.ToString(output.Policy.Arn),
		"versionId", aws.ToString(output.Policy.DefaultVersionId))

	return output.Policy, nil
}

// createPolicyVersion creates a new version of an existing policy and returns the version ID.
// Returns the current version ID if policy document is unchanged (no new version created).
func (op *IamPolicyOperations) createPolicyVersion(
	ctx context.Context, policyArn, desiredDocument string) (string, error) {

	log := logf.FromContext(ctx).WithValues("policyArn", policyArn)

	log.Info("Checking if policy update needed")

	// Get current default version to check for drift
	currentDocument, versionId, err := op.getCurrentPolicyDocument(ctx, policyArn)
	if err != nil {
		return "", fmt.Errorf("failed to get current policy document: %w", err)
	}

	// Compare documents using semantic equality (handles whitespace, key ordering)
	if jsonEquals(currentDocument, desiredDocument) {
		log.Info("Policy document unchanged, skipping version creation", "versionId", versionId)
		return versionId, nil
	}

	log.Info("Policy document changed, creating new version")

	// Check current version count and cleanup if needed
	if err := op.deleteOldestPolicyVersion(ctx, policyArn); err != nil {
		return "", fmt.Errorf("failed to cleanup old versions: %w", err)
	}

	// Create new version
	input := &iam.CreatePolicyVersionInput{
		PolicyArn:      aws.String(policyArn),
		PolicyDocument: aws.String(desiredDocument),
		SetAsDefault:   true, // Set new version as default
	}

	output, err := op.iamClient.CreatePolicyVersion(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to create policy version: %w", err)
	}

	newVersionId := aws.ToString(output.PolicyVersion.VersionId)

	log.Info("Successfully created policy version", "versionId", newVersionId)

	return newVersionId, nil
}

// deleteOldestPolicyVersion removes oldest non-default version if at 5 version limit
func (op *IamPolicyOperations) deleteOldestPolicyVersion(ctx context.Context, policyArn string) error {
	log := logf.FromContext(ctx).WithValues("policyArn", policyArn)

	// List all versions
	listInput := &iam.ListPolicyVersionsInput{
		PolicyArn: aws.String(policyArn),
	}

	listOutput, err := op.iamClient.ListPolicyVersions(ctx, listInput)
	if err != nil {
		return fmt.Errorf("failed to list policy versions: %w", err)
	}

	// AWS allows max 5 versions
	if len(listOutput.Versions) < MaxNumberOfPolicyVersions {
		log.V(1).Info("Version count under limit, no cleanup needed", "currentVersions", len(listOutput.Versions))
		return nil
	}

	// Find oldest non-default version to delete
	var oldestVersion *types.PolicyVersion
	for i := range listOutput.Versions {
		v := &listOutput.Versions[i]

		if v.IsDefaultVersion {
			continue
		}

		if oldestVersion == nil || v.CreateDate.Before(*oldestVersion.CreateDate) {
			oldestVersion = v
		}
	}

	if oldestVersion == nil {
		log.V(1).Info("No non-default versions to delete")
		return nil
	}

	// Delete oldest version
	deleteInput := &iam.DeletePolicyVersionInput{
		PolicyArn: aws.String(policyArn),
		VersionId: oldestVersion.VersionId,
	}

	_, err = op.iamClient.DeletePolicyVersion(ctx, deleteInput)
	if err != nil {
		return fmt.Errorf("failed to delete old policy version %s: %w", aws.ToString(oldestVersion.VersionId), err)
	}

	log.Info("Deleted oldest policy version to make room for new version",
		"deletedVersion", aws.ToString(oldestVersion.VersionId))

	return nil
}

// getCurrentPolicyDocument retrieves the current default policy document
func (op *IamPolicyOperations) getCurrentPolicyDocument(ctx context.Context, policyArn string) (string, string, error) {
	// First get policy to find default version
	policy, err := op.getPolicyByArn(ctx, policyArn)
	if err != nil {
		return "", "", err
	}
	if policy == nil {
		return "", "", fmt.Errorf("policy not found")
	}

	defaultVersionId := aws.ToString(policy.DefaultVersionId)

	// Get the policy version document
	input := &iam.GetPolicyVersionInput{
		PolicyArn: aws.String(policyArn),
		VersionId: aws.String(defaultVersionId),
	}

	output, err := op.iamClient.GetPolicyVersion(ctx, input)
	if err != nil {
		return "", "", fmt.Errorf("failed to get policy version: %w", err)
	}

	document := aws.ToString(output.PolicyVersion.Document)
	return document, defaultVersionId, nil
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
