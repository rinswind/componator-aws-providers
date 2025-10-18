// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package iamrole

import (
	"context"
	"fmt"

	"github.com/rinswind/deployment-operator/componentkit/controller"
)

// Delete removes the IAM role after detaching all managed policies.
// This method handles:
// - Listing all attached managed policies
// - Detaching each policy
// - Deleting the role
func (op *IamRoleOperations) Delete(ctx context.Context) (*controller.ActionResult, error) {
	// TODO: Implement in Phase 3
	// - List attached policies (ListAttachedRolePolicies)
	// - Detach each policy (DetachRolePolicy)
	// - Delete role (DeleteRole)
	// - return controller.ActionSuccess(op.status)
	// - On error: return controller.ActionResultForError(op.status, err, iamErrorClassifier)
	return controller.ActionFailure(op.status, fmt.Errorf("Delete not yet implemented"))
}

// CheckDeletion verifies the IAM role has been successfully deleted.
func (op *IamRoleOperations) CheckDeletion(ctx context.Context) (*controller.CheckResult, error) {
	// TODO: Implement in Phase 3
	// - Check if role still exists using getRoleByName
	// - If not found: return controller.CheckComplete(op.status)
	// - If exists: return controller.CheckInProgress(op.status)
	// - On error: return controller.CheckResultForError(op.status, err, iamErrorClassifier)
	return controller.CheckFailure(op.status, fmt.Errorf("CheckDeletion not yet implemented"))
}
