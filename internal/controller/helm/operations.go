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
	"context"
	"fmt"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

// performHelmDeployment handles all Helm-specific deployment operations
func (r *ComponentReconciler) performHelmDeployment(ctx context.Context, component *deploymentsv1alpha1.Component) error {
	log := logf.FromContext(ctx)

	// Generate deterministic release name
	releaseName := r.generateReleaseName(component)

	// Parse the Helm configuration from Component.Spec.Config
	config, err := r.parseHelmConfig(component)
	if err != nil {
		return fmt.Errorf("failed to parse helm configuration: %w", err)
	}

	// Determine target namespace (use config.Namespace if specified, otherwise component namespace)
	targetNamespace := component.Namespace
	if config.Namespace != "" {
		targetNamespace = config.Namespace
	}

	log.Info("Parsed helm configuration",
		"repository", config.Repository.URL,
		"chart", config.Chart.Name,
		"version", config.Chart.Version,
		"releaseName", releaseName,
		"targetNamespace", targetNamespace,
		"valuesCount", len(config.Values))

	// TODO: Implement actual Helm deployment logic in later tasks
	// For now, just return success after configuration parsing and validation

	return nil
}

// performHelmCleanup handles all Helm-specific cleanup operations
func (r *ComponentReconciler) performHelmCleanup(ctx context.Context, component *deploymentsv1alpha1.Component) error {
	log := logf.FromContext(ctx)

	// Generate deterministic release name (same as deployment)
	releaseName := r.generateReleaseName(component)

	// Determine target namespace - we need this to clean up from the correct namespace
	// Try to get it from config, but don't fail cleanup if config is invalid
	targetNamespace := component.Namespace
	if config, err := r.parseHelmConfig(component); err == nil && config.Namespace != "" {
		targetNamespace = config.Namespace
	}

	log.Info("Performing helm cleanup",
		"releaseName", releaseName,
		"targetNamespace", targetNamespace)

	// TODO: Implement actual Helm uninstallation logic in later tasks
	// This should include:
	// - helm uninstall <releaseName> --namespace <targetNamespace>
	// - Wait for resources to be cleaned up
	// - Handle cases where release doesn't exist (already cleaned up)

	return nil
}
