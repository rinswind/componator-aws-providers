// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package iamrole

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

// Package-level singletons initialized during registration
var (
	iamClient *iam.Client
)

// getRoleByName retrieves role by name
func getRoleByName(ctx context.Context, roleName string) (*types.Role, error) {
	input := &iam.GetRoleInput{
		RoleName: aws.String(roleName),
	}

	output, err := iamClient.GetRole(ctx, input)
	if err == nil {
		return output.Role, nil
	}

	// Check if role not found
	if isNotFoundError(err) {
		return nil, nil
	}

	return nil, fmt.Errorf("failed to get role: %w", err)
}

// listAttachedPolicies retrieves all managed policies currently attached to the role
func listAttachedPolicies(ctx context.Context, roleName string) ([]string, error) {
	input := &iam.ListAttachedRolePoliciesInput{
		RoleName: aws.String(roleName),
	}

	output, err := iamClient.ListAttachedRolePolicies(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to list attached policies: %w", err)
	}

	arns := make([]string, 0, len(output.AttachedPolicies))
	for i := range output.AttachedPolicies {
		arns = append(arns, aws.ToString(output.AttachedPolicies[i].PolicyArn))
	}

	return arns, nil
}

// createRole creates a new IAM role with the specified trust policy and returns the created role
func createRole(
	ctx context.Context,
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

	output, err := iamClient.CreateRole(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to create role: %w", err)
	}

	log.Info("Successfully created IAM role", "roleArn", aws.ToString(output.Role.Arn))

	return output.Role, nil
}

// updateTrustPolicy updates the assume role policy document for an existing role
func updateTrustPolicy(ctx context.Context, roleName, currentPolicy, desiredPolicy string) error {
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

	_, err := iamClient.UpdateAssumeRolePolicy(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to update assume role policy: %w", err)
	}

	log.Info("Successfully updated trust policy")
	return nil
}

// attachPolicy attaches a managed policy to the role
func attachPolicy(ctx context.Context, roleName, policyArn string) error {
	input := &iam.AttachRolePolicyInput{
		RoleName:  aws.String(roleName),
		PolicyArn: aws.String(policyArn),
	}

	_, err := iamClient.AttachRolePolicy(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to attach policy: %w", err)
	}

	return nil
}

// detachPolicy detaches a managed policy from the role
func detachPolicy(ctx context.Context, roleName, policyArn string) error {
	input := &iam.DetachRolePolicyInput{
		RoleName:  aws.String(roleName),
		PolicyArn: aws.String(policyArn),
	}

	_, err := iamClient.DetachRolePolicy(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to detach policy: %w", err)
	}

	return nil
}

// deleteRole deletes the IAM role
func deleteRole(ctx context.Context, roleName string) error {
	input := &iam.DeleteRoleInput{
		RoleName: aws.String(roleName),
	}

	_, err := iamClient.DeleteRole(ctx, input)
	if err != nil {
		// If role already deleted, treat as success
		if isNotFoundError(err) {
			return nil
		}
		return fmt.Errorf("failed to delete role: %w", err)
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

// isNotFoundError checks if error indicates role not found
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
