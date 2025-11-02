// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package iampolicy

import (
	"time"

	componentkit "github.com/rinswind/componator/componentkit/controller"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	DefaultProviderName = "iam-policy"
)

// Register creates and registers an IAM Policy component provider controller with the manager.
// This is the primary API for embedding the IAM Policy provider in setkit distributions.
//
// Parameters:
//   - mgr: The controller-runtime manager that provides client, scheme, and controller registration.
//   - providerName: The full provider name to register. Use "" to register with the default name "iam-policy".
//     For setkit embedding, use a prefixed name (e.g., "wordpress-iam-policy") to avoid conflicts.
//
// CRITICAL: Provider names must be unique across all providers in the cluster. Multiple providers with
// the same name will conflict and cause undefined behavior in Component claiming and status reporting.
//
// Returns:
//   - error: Initialization or registration errors.
//
// Example standalone usage (in componator-aws-providers/cmd/main.go):
//
//	if err := iampolicy.Register(mgr, ""); err != nil {
//	    setupLog.Error(err, "unable to register iam-policy controller")
//	    os.Exit(1)
//	}
//
// Example setkit usage (in wordpress-operator/cmd/main.go):
//
//	if err := iampolicy.Register(mgr, "wordpress-iam-policy"); err != nil {
//	    setupLog.Error(err, "unable to register iam-policy provider")
//	    os.Exit(1)
//	}
func Register(mgr ctrl.Manager, providerName string) error {
	// Use default provider name if not specified
	if providerName == "" {
		providerName = DefaultProviderName
	}

	// Create operations factory and config
	factory := NewIamPolicyOperationsFactory()
	config := componentkit.DefaultComponentReconcilerConfig(providerName)
	config.ErrorRequeue = 30 * time.Second       // Give more time for AWS throttling errors
	config.DefaultRequeue = 15 * time.Second     // IAM operations are generally fast
	config.StatusCheckRequeue = 10 * time.Second // Check policy status frequently

	// Register directly with componentkit
	return componentkit.Register(mgr, factory, config, nil)
}
