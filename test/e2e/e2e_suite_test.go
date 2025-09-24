//go:build e2e
// +build e2e

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

package e2e

import (
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// TestE2E runs the Helm Handler e2e tests against a real Kubernetes cluster.
// The controller runs locally within the test process, similar to integration tests
// but against real Kubernetes instead of envtest.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting Helm Handler E2E test suite\n")
	RunSpecs(t, "Helm Handler E2E")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	_, _ = fmt.Fprintf(GinkgoWriter, "Running Helm Handler controller locally against real cluster\n")
})

var _ = AfterSuite(func() {
	_, _ = fmt.Fprintf(GinkgoWriter, "Completed Helm Handler E2E test suite\n")
})
