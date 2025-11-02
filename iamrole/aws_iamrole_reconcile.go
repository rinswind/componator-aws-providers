// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package iamrole

import (
	"context"
	"fmt"
	"maps"
	"slices"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// PolicyReconciliationResult contains the outcome of policy attachment reconciliation
type PolicyReconciliationResult struct {
	AttachedPolicies []string // Final list of attached policies
	AttachedCount    int      // Number of policies attached during this operation
	DetachedCount    int      // Number of policies detached during this operation
}

// reconcilePolicyAttachments ensures the role has exactly the desired managed policies attached.
// Returns the actual list of attached policies after reconciliation (which may be partial on failure).
// This allows status to reflect reality even when reconciliation fails partway through.
func reconcilePolicyAttachments(ctx context.Context, roleName string, desiredPolicies []string) (*PolicyReconciliationResult, error) {
	log := logf.FromContext(ctx).WithValues("roleName", roleName)

	// Get currently attached policies
	currentPolicies, err := listAttachedPolicies(ctx, roleName)
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
		if err := detachPolicy(ctx, roleName, arn); err != nil {
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
		if err := attachPolicy(ctx, roleName, arn); err != nil {
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
