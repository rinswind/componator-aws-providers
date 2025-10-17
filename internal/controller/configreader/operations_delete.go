// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package configreader

import (
	"context"

	"github.com/rinswind/deployment-operator/componentkit/controller"
)

// Delete is a no-op for config-reader - no resources to clean up.
// Config-reader only reads values, it doesn't create any Kubernetes resources.
func (o *ConfigReaderOperations) Delete(ctx context.Context) (*controller.ActionResult, error) {
	return o.actionSuccessResult()
}

// CheckDeletion always returns success immediately since there's nothing to clean up.
// Config-reader has no resources to wait for deletion.
func (o *ConfigReaderOperations) CheckDeletion(ctx context.Context) (*controller.CheckResult, error) {
	return o.checkCompleteResult()
}
