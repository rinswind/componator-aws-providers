// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package rds

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

// Register creates and registers an RDS component provider controller with the manager.
// This is the primary API for embedding the RDS provider in setkit distributions.
//
// Parameters:
//   - mgr: The controller-runtime manager that provides client, scheme, and controller registration.
//   - namespace: The setkit namespace for provider isolation. Use "" for standalone deployment (provider name becomes "rds").
//     For setkit embedding, use the setkit name (e.g., "wordpress") to create provider "wordpress-rds".
//
// Returns:
//   - error: Initialization or registration errors.
//
// Example standalone usage (in componator-aws-providers/cmd/main.go):
//
//	if err := rds.Register(mgr, ""); err != nil {
//	    setupLog.Error(err, "unable to register rds controller")
//	    os.Exit(1)
//	}
//
// Example setkit usage (in wordpress-operator/cmd/main.go):
//
//	if err := rds.Register(mgr, "wordpress"); err != nil {
//	    setupLog.Error(err, "unable to register rds provider")
//	    os.Exit(1)
//	}
func Register(mgr ctrl.Manager, namespace string) error {
	// Determine provider name based on namespace
	providerName := HandlerName
	if namespace != "" {
		providerName = namespace + "-" + HandlerName
	}

	// Create controller with namespaced provider name
	controller := NewComponentReconciler(providerName)

	// Register with manager
	return controller.SetupWithManager(mgr)
}
