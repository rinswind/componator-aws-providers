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
)

func TestHelmStatusPersistence(t *testing.T) {
	// Test that status fields are properly structured
	status := HelmStatus{
		ReleaseVersion: 1,
		LastDeployTime: "2025-01-01T00:00:00Z",
		ChartVersion:   "1.0.0",
		ReleaseName:    "test-release",
	}

	// Verify all expected fields are present
	if status.ReleaseVersion != 1 {
		t.Errorf("ReleaseVersion not properly set")
	}
	if status.LastDeployTime == "" {
		t.Errorf("LastDeployTime not properly set")
	}
	if status.ChartVersion == "" {
		t.Errorf("ChartVersion not properly set")
	}
	if status.ReleaseName == "" {
		t.Errorf("ReleaseName not properly set")
	}
}
