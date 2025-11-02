// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package iampolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/rinswind/componator/componentkit/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	MaxNumberOfPolicyVersions = 5
)

// Package-level singletons initialized during registration
var (
	awsConfig aws.Config
	iamClient *iam.Client
)

// getPolicyByName retrieves policy by name (searches by path and name)
func getPolicyByName(ctx context.Context, policyName, path string) (*types.Policy, error) {
	// List policies to find a match
	input := &iam.ListPoliciesInput{
		Scope:      types.PolicyScopeTypeLocal,
		PathPrefix: aws.String(path),
	}

	output, err := iamClient.ListPolicies(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to list policies: %w", err)
	}

	// Find policy with matching name
	for i := range output.Policies {
		policy := &output.Policies[i]
		if aws.ToString(policy.PolicyName) == policyName {
			return policy, nil
		}
	}

	return nil, nil // Not found
}

// getPolicyByArn retrieves policy by ARN
func getPolicyByArn(ctx context.Context, arn string) (*types.Policy, error) {
	input := &iam.GetPolicyInput{
		PolicyArn: aws.String(arn),
	}

	output, err := iamClient.GetPolicy(ctx, input)
	if err == nil {
		return output.Policy, nil
	}

	// Check if policy not found
	if isNotFoundError(err) {
		return nil, nil
	}

	return nil, fmt.Errorf("failed to get policy: %w", err)
}

// createPolicy creates a new IAM policy and returns the created policy
func createPolicy(
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

	output, err := iamClient.CreatePolicy(ctx, input)
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
func createPolicyVersion(
	ctx context.Context, policyArn, desiredDocument string) (string, error) {

	log := logf.FromContext(ctx).WithValues("policyArn", policyArn)

	log.Info("Checking if policy update needed")

	// Get current default version to check for drift
	currentDocument, versionId, err := getCurrentPolicyDocument(ctx, policyArn)
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
	if err := deleteOldestPolicyVersion(ctx, policyArn); err != nil {
		return "", fmt.Errorf("failed to cleanup old versions: %w", err)
	}

	// Create new version
	input := &iam.CreatePolicyVersionInput{
		PolicyArn:      aws.String(policyArn),
		PolicyDocument: aws.String(desiredDocument),
		SetAsDefault:   true, // Set new version as default
	}

	output, err := iamClient.CreatePolicyVersion(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to create policy version: %w", err)
	}

	newVersionId := aws.ToString(output.PolicyVersion.VersionId)

	log.Info("Successfully created policy version", "versionId", newVersionId)

	return newVersionId, nil
}

// deleteOldestPolicyVersion removes oldest non-default version if at 5 version limit
func deleteOldestPolicyVersion(ctx context.Context, policyArn string) error {
	log := logf.FromContext(ctx).WithValues("policyArn", policyArn)

	// List all versions
	listInput := &iam.ListPolicyVersionsInput{
		PolicyArn: aws.String(policyArn),
	}

	listOutput, err := iamClient.ListPolicyVersions(ctx, listInput)
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

	_, err = iamClient.DeletePolicyVersion(ctx, deleteInput)
	if err != nil {
		return fmt.Errorf("failed to delete old policy version %s: %w", aws.ToString(oldestVersion.VersionId), err)
	}

	log.Info("Deleted oldest policy version to make room for new version",
		"deletedVersion", aws.ToString(oldestVersion.VersionId))

	return nil
}

// getCurrentPolicyDocument retrieves the current default policy document
func getCurrentPolicyDocument(ctx context.Context, policyArn string) (string, string, error) {
	// First get policy to find default version
	policy, err := getPolicyByArn(ctx, policyArn)
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

	output, err := iamClient.GetPolicyVersion(ctx, input)
	if err != nil {
		return "", "", fmt.Errorf("failed to get policy version: %w", err)
	}

	document := aws.ToString(output.PolicyVersion.Document)
	return document, defaultVersionId, nil
}

// deletePolicy deletes an IAM policy by ARN
func deletePolicy(ctx context.Context, policyArn string) error {
	log := logf.FromContext(ctx).WithValues("policyArn", policyArn)

	deleteInput := &iam.DeletePolicyInput{
		PolicyArn: aws.String(policyArn),
	}

	_, err := iamClient.DeletePolicy(ctx, deleteInput)
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
func deletePolicyAllVersions(ctx context.Context, policyArn string) error {
	log := logf.FromContext(ctx).WithValues("policyArn", policyArn)

	// List all versions
	listInput := &iam.ListPolicyVersionsInput{
		PolicyArn: aws.String(policyArn),
	}

	listOutput, err := iamClient.ListPolicyVersions(ctx, listInput)
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

		_, err := iamClient.DeletePolicyVersion(ctx, deleteInput)
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

// isNotFoundError checks if error indicates policy not found
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	// Check for IAM NoSuchEntity error
	var notFoundErr *types.NoSuchEntityException
	return errors.As(err, &notFoundErr)
}

// iamErrorClassifier wraps the AWS SDK retry logic for use with result builder utilities.
var iamErrorClassifier = controller.ErrorClassifier(isRetryable)

// isRetryable determines if an error is retryable using AWS SDK's built-in error classification.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Use AWS SDK's built-in retry classification
	// This handles all AWS API errors, network errors, and HTTP status codes
	for _, checker := range retry.DefaultRetryables {
		if checker.IsErrorRetryable(err) == aws.TrueTernary {
			return true
		}
	}

	return false
}
