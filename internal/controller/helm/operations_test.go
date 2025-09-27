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
	"testing"

	"github.com/rinswind/deployment-handlers/internal/controller/base"
)

func TestHelmOperationsImplementsInterface(t *testing.T) {
	// Compile-time check that HelmOperations implements ComponentOperations
	var _ base.ComponentOperations = (*HelmOperations)(nil)
	
	// Verify we can create an instance
	ops := NewHelmOperations()
	if ops == nil {
		t.Error("NewHelmOperations() returned nil")
	}
}

func TestHelmOperationsConfigImplementsInterface(t *testing.T) {
	// Compile-time check that HelmOperationsConfig implements ComponentOperationsConfig
	var _ base.ComponentOperationsConfig = (*HelmOperationsConfig)(nil)
	
	// Verify we can create an instance and get expected values
	config := NewHelmOperationsConfig()
	if config == nil {
		t.Error("NewHelmOperationsConfig() returned nil")
	}
	
	if config.GetHandlerName() != HandlerName {
		t.Errorf("Expected handler name %s, got %s", HandlerName, config.GetHandlerName())
	}
	
	if config.GetControllerName() != ControllerName {
		t.Errorf("Expected controller name %s, got %s", ControllerName, config.GetControllerName())
	}
	
	// Verify requeue settings
	settings := config.GetRequeueSettings()
	if settings.DefaultRequeue <= 0 {
		t.Error("Expected positive default requeue duration")
	}
}
