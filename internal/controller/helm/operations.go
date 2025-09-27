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

package helm

import (
	"github.com/rinswind/deployment-handlers/internal/controller/base"
)

const (
	// HandlerName is the identifier for this helm handler
	HandlerName = "helm"

	ControllerName = "helm-component"
)

// HelmOperations implements the ComponentOperations interface for Helm-based deployments.
// This struct provides all Helm-specific deployment, upgrade, and deletion operations
// with implementations organized across domain-specific files.
type HelmOperations struct {
	// Future: could add Helm-specific configuration or clients here
}

// NewHelmOperations creates a new HelmOperations instance
func NewHelmOperations() *HelmOperations {
	return &HelmOperations{}
}

// NewHelmOperationsConfig creates a ComponentHandlerConfig for Helm with default settings
func NewHelmOperationsConfig() base.ComponentHandlerConfig {
	return base.DefaultComponentHandlerConfig(HandlerName, ControllerName)
}
