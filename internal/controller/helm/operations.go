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

const (
	// DeploymentNamespaceAnnotation stores the actual namespace where Helm release was deployed
	DeploymentNamespaceAnnotation = "helm.deployment-orchestrator.io/target-namespace"
	// DeploymentReleaseNameAnnotation stores the actual release name used for Helm deployment
	DeploymentReleaseNameAnnotation = "helm.deployment-orchestrator.io/release-name"
)

// performHelmDeployment handles all Helm-specific deployment operations
// Returns a map of annotations that should be set on the Component
func (r *ComponentReconciler) performHelmDeployment(ctx context.Context, component *deploymentsv1alpha1.Component) (map[string]string, error) {
	log := logf.FromContext(ctx)

	// Parse the Helm configuration from Component.Spec.Config
	config, err := r.parseHelmConfig(component)
	if err != nil {
		return nil, fmt.Errorf("failed to parse helm configuration: %w", err)
	}

	// Generate deterministic release name
	releaseName := r.generateReleaseName(component)

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

	// Return annotations that should be set on the Component
	annotations := map[string]string{
		DeploymentNamespaceAnnotation:   targetNamespace,
		DeploymentReleaseNameAnnotation: releaseName,
	}

	return annotations, nil
}

// performHelmCleanup handles all Helm-specific cleanup operations
func (r *ComponentReconciler) performHelmCleanup(ctx context.Context, component *deploymentsv1alpha1.Component) error {
	log := logf.FromContext(ctx)

	// Get release name from stored annotation
	releaseName := component.Annotations[DeploymentReleaseNameAnnotation]
	if releaseName == "" {
		return fmt.Errorf("release name annotation %s not found - component may not have been properly deployed", DeploymentReleaseNameAnnotation)
	}

	// Get target namespace from stored annotation
	targetNamespace := component.Annotations[DeploymentNamespaceAnnotation]
	if targetNamespace == "" {
		return fmt.Errorf("target namespace annotation %s not found - component may not have been properly deployed", DeploymentNamespaceAnnotation)
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
