/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package base

import (
	"context"
	"time"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

// ComponentOperations defines the interface that all Component handlers must implement
// to provide technology-specific deployment operations while working with the generic
// controller protocol logic.
//
// This interface separates handler-specific deployment logic from the generic protocol
// state machine, enabling code reuse across different deployment technologies (Helm, Terraform, etc.)
type ComponentOperations interface {
	// Deploy initiates the initial deployment of a Component's resources.
	// This is called when a Component transitions from Claimed to Deploying state.
	//
	// The implementation should start the deployment process asynchronously and return
	// immediately without waiting for completion. Status checking is handled separately
	// via CheckDeployment.
	//
	// Returns:
	//   - error: any error that prevents deployment from starting (treated as permanent failure)
	Deploy(ctx context.Context, component *deploymentsv1alpha1.Component) error

	// CheckDeployment verifies the current deployment status and readiness.
	// This is called repeatedly while a Component is in Deploying state.
	//
	// The implementation should perform non-blocking checks of the deployment status
	// and return the current state without waiting for completion.
	//
	// Parameters:
	//   - elapsed: time elapsed since deployment started (for timeout checking)
	//
	// Returns:
	//   - ready: true if deployment is complete and resources are ready
	//   - ioError: transient I/O or communication errors (causes requeue)
	//   - deploymentError: permanent deployment failures (causes Failed state)
	CheckDeployment(ctx context.Context, component *deploymentsv1alpha1.Component, elapsed time.Duration) (ready bool, ioError error, deploymentError error)

	// Upgrade initiates an upgrade of an existing deployment with new configuration.
	// This is called when a Component in Ready or Failed state has changes (is "dirty").
	//
	// The implementation should start the upgrade process asynchronously and return
	// immediately without waiting for completion. The Component will transition back to
	// Deploying state and CheckDeployment will be used to monitor progress.
	//
	// Returns:
	//   - error: any error that prevents upgrade from starting (treated as permanent failure)
	Upgrade(ctx context.Context, component *deploymentsv1alpha1.Component) error

	// Delete initiates cleanup/deletion of a Component's resources.
	// This is called when a Component transitions to Terminating state during deletion.
	//
	// The implementation should start the cleanup process asynchronously and return
	// immediately without waiting for completion. Status checking is handled separately
	// via CheckDeletion.
	//
	// Returns:
	//   - error: any error during cleanup initiation (logged but deletion continues)
	Delete(ctx context.Context, component *deploymentsv1alpha1.Component) error

	// CheckDeletion verifies the current deletion status and completion.
	// This is called repeatedly while a Component is in Terminating state.
	//
	// The implementation should perform non-blocking checks of the deletion status
	// and return the current state without waiting for completion.
	//
	// Parameters:
	//   - elapsed: time elapsed since deletion started (for timeout checking)
	//
	// Returns:
	//   - deleted: true if all resources have been successfully removed
	//   - ioError: transient I/O or communication errors (causes requeue)
	//   - deletionError: permanent deletion failures (causes permanent Terminating state)
	CheckDeletion(ctx context.Context, component *deploymentsv1alpha1.Component, elapsed time.Duration) (deleted bool, ioError error, deletionError error)
}

// ComponentOperationsConfig provides handler-specific configuration for the generic controller.
// This allows handlers to customize controller behavior without changing the core protocol logic.
type ComponentOperationsConfig interface {
	// GetHandlerName returns the identifier for this handler (e.g., "helm", "rds")
	// Used for finalizer names, logging, and component filtering.
	GetHandlerName() string

	// GetControllerName returns the controller name for registration with the manager
	// Used in controller-runtime setup and metrics.
	GetControllerName() string

	// GetRequeueSettings returns timing configuration for requeue operations
	GetRequeueSettings() RequeueSettings
}

// RequeueSettings defines timing configuration for controller requeue operations
type RequeueSettings struct {
	// DefaultRequeue is the default requeue period for normal operations
	DefaultRequeue time.Duration

	// ErrorRequeue is the requeue period for transient errors
	ErrorRequeue time.Duration

	// StatusCheckRequeue is the requeue period for status checking operations
	StatusCheckRequeue time.Duration
}

// DefaultRequeueSettings provides sensible defaults for requeue timing
func DefaultRequeueSettings() RequeueSettings {
	return RequeueSettings{
		DefaultRequeue:     5 * time.Second,
		ErrorRequeue:       10 * time.Second,
		StatusCheckRequeue: 5 * time.Second,
	}
}
