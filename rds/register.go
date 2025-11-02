// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package rds

import (
	"time"

	componentkit "github.com/rinswind/componator/componentkit/controller"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	DefaultProviderName = "rds"
)

// Register creates and registers an RDS component provider controller with the manager.
// This is the primary API for embedding the RDS provider in setkit distributions.
//
// Parameters:
//   - mgr: The controller-runtime manager that provides client, scheme, and controller registration.
//   - providerName: The full provider name to register. Use "" to register with the default name "rds".
//     For setkit embedding, use a prefixed name (e.g., "wordpress-rds") to avoid conflicts.
//
// CRITICAL: Provider names must be unique across all providers in the cluster. Multiple providers with
// the same name will conflict and cause undefined behavior in Component claiming and status reporting.
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
//	if err := rds.Register(mgr, "wordpress-rds"); err != nil {
//	    setupLog.Error(err, "unable to register rds provider")
//	    os.Exit(1)
//	}
func Register(mgr ctrl.Manager, providerName string) error {
	// Use default provider name if not specified
	if providerName == "" {
		providerName = DefaultProviderName
	}

	// Create operations factory and config
	factory := NewRdsOperationsFactory()
	config := componentkit.DefaultComponentReconcilerConfig(providerName)
	config.ErrorRequeue = 15 * time.Second
	config.DefaultRequeue = 30 * time.Second
	config.StatusCheckRequeue = 30 * time.Second

	// Register directly with componentkit
	return componentkit.Register(mgr, factory, config, nil)
}
