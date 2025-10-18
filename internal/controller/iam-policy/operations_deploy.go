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
	existingPolicy, err := op.getPolicyByName(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check if policy exists: %w", err)
	}

	if existingPolicy == nil {
		// Policy doesn't exist - create it
		return op.createPolicy(ctx)
	}

	// Policy exists - update status and create new version
	op.status.PolicyArn = aws.ToString(existingPolicy.Arn)
	op.status.PolicyId = aws.ToString(existingPolicy.PolicyId)
	op.status.PolicyName = aws.ToString(existingPolicy.PolicyName)

	log.Info("Policy already exists, will update via versioning", "policyArn", op.status.PolicyArn)

	return op.createPolicyVersion(ctx, op.status.PolicyArn)
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

	return controller.CheckComplete(op.status)
}

// createPolicy creates a new IAM policy
func (op *IamPolicyOperations) createPolicy(ctx context.Context) (*controller.ActionResult, error) {
	log := logf.FromContext(ctx).WithValues("policyName", op.config.PolicyName)

	log.Info("Creating new IAM policy")

	input := &iam.CreatePolicyInput{
		PolicyName:     aws.String(op.config.PolicyName),
		PolicyDocument: aws.String(op.config.PolicyDocument),
		Path:           aws.String(op.config.Path),
	}

	if op.config.Description != "" {
		input.Description = aws.String(op.config.Description)
	}

	if len(op.config.Tags) > 0 {
		input.Tags = toIAMTags(op.config.Tags)
	}

	output, err := op.iamClient.CreatePolicy(ctx, input)
	if err != nil {
		return controller.ActionResultForError(op.status,
			fmt.Errorf("failed to create policy: %w", err), iamErrorClassifier)
	}

	// Update status with created policy info
	op.status.PolicyArn = aws.ToString(output.Policy.Arn)
	op.status.PolicyId = aws.ToString(output.Policy.PolicyId)
	op.status.PolicyName = aws.ToString(output.Policy.PolicyName)
	op.status.CurrentVersionId = aws.ToString(output.Policy.DefaultVersionId)

	log.Info("Successfully created IAM policy",
		"policyArn", op.status.PolicyArn,
		"versionId", op.status.CurrentVersionId)

	return controller.ActionSuccess(op.status)
}

// createPolicyVersion creates a new version of an existing policy
func (op *IamPolicyOperations) createPolicyVersion(ctx context.Context, policyArn string) (*controller.ActionResult, error) {
	log := logf.FromContext(ctx).WithValues("policyArn", policyArn)

	log.Info("Checking if policy update needed")

	// Get current default version to check for drift
	currentDocument, versionId, err := op.getCurrentPolicyDocument(ctx, policyArn)
	if err != nil {
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to get current policy document: %w", err), iamErrorClassifier)
	}

	// Compare documents using semantic equality (handles whitespace, key ordering)
	if jsonEquals(currentDocument, op.config.PolicyDocument) {
		// No changes detected - update status and return success
		op.status.CurrentVersionId = versionId
		log.Info("Policy document unchanged, skipping version creation", "versionId", versionId)
		return controller.ActionSuccess(op.status)
	}

	log.Info("Policy document changed, creating new version")

	// Check current version count and cleanup if needed
	if err := op.deleteOldestPolicyVersion(ctx, policyArn); err != nil {
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to cleanup old versions: %w", err), iamErrorClassifier)
	}

	// Create new version
	input := &iam.CreatePolicyVersionInput{
		PolicyArn:      aws.String(policyArn),
		PolicyDocument: aws.String(op.config.PolicyDocument),
		SetAsDefault:   true, // Set new version as default
	}

	output, err := op.iamClient.CreatePolicyVersion(ctx, input)
	if err != nil {
		return controller.ActionResultForError(
			op.status, fmt.Errorf("failed to create policy version: %w", err), iamErrorClassifier)
	}

	op.status.CurrentVersionId = aws.ToString(output.PolicyVersion.VersionId)

	log.Info("Successfully created policy version", "versionId", op.status.CurrentVersionId)

	return controller.ActionSuccess(op.status)
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
