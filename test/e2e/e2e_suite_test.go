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
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

// Shared variables for all e2e tests
var (
	ctx       context.Context
	cancel    context.CancelFunc
	k8sClient client.Client
)

// TestE2E runs both Helm and RDS handler e2e tests against a real Kubernetes cluster.
// Each handler controller runs locally within the test process, similar to integration tests
// but against real Kubernetes instead of envtest.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting Component Handler E2E test suite\n")
	RunSpecs(t, "Component Handler E2E")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.Background())

	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	_, _ = fmt.Fprintf(GinkgoWriter, "Running Component Handler controllers locally against real cluster\n")

	// Register our API types with the scheme
	err := deploymentsv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred(), "Failed to add deployment API to scheme")
})

var _ = AfterSuite(func() {
	_, _ = fmt.Fprintf(GinkgoWriter, "Completed Component Handler E2E test suite\n")
	if cancel != nil {
		cancel()
	}
})
