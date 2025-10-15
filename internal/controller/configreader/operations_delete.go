// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package configreader

import (
	"context"

	"github.com/rinswind/deployment-operator/componentkit/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Delete is a no-op for config-reader since it creates no resources.
// Returns success immediately.
func (o *ConfigReaderOperations) Delete(ctx context.Context) (*controller.OperationResult, error) {
	log := logf.FromContext(ctx)
	log.V(1).Info("Delete called - config-reader creates no resources, nothing to clean up")
	return o.successResult(), nil
}

// CheckDeletion always returns success immediately since Delete is a no-op.
// Config-reader has no resources to wait for cleanup.
func (o *ConfigReaderOperations) CheckDeletion(ctx context.Context) (*controller.OperationResult, error) {
	return o.successResult(), nil
}
