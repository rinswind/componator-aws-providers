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

package rds

import (
	"time"

	"github.com/rinswind/deployment-operator/handler/base"
)

const (
	// HandlerName is the identifier for this RDS handler
	HandlerName = "rds"

	ControllerName = "rds-component"
)

// RdsOperations implements the ComponentOperations interface for RDS-based deployments.
// This struct provides all RDS-specific deployment, upgrade, and deletion operations
// for managing AWS RDS instances through the AWS SDK.
type RdsOperations struct {
	// TODO: Add AWS SDK clients and configuration when implementing actual RDS operations
	// For example:
	// - rdsClient *rds.Client
	// - region string
	// - credentials aws.CredentialsProvider
}

// NewRdsOperations creates a new RdsOperations instance
func NewRdsOperations() *RdsOperations {
	return &RdsOperations{}
}

// NewRdsOperationsConfig creates a ComponentHandlerConfig for RDS with appropriate settings
func NewRdsOperationsConfig() base.ComponentHandlerConfig {
	config := base.DefaultComponentHandlerConfig(HandlerName, ControllerName)

	// RDS operations typically take longer than Helm operations
	// Adjust timeouts to account for database creation/modification times
	config.DefaultRequeue = 30 * time.Second     // RDS operations are slower
	config.StatusCheckRequeue = 15 * time.Second // Check database status less frequently
	config.ErrorRequeue = 30 * time.Second       // Give more time for transient errors

	return config
}
