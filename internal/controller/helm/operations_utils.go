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
	"bytes"
	"context"
	"fmt"

	deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/release"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// DeploymentNamespaceAnnotation stores the actual namespace where Helm release was deployed
	DeploymentNamespaceAnnotation = "helm.deployment-orchestrator.io/target-namespace"
	// DeploymentReleaseNameAnnotation stores the actual release name used for Helm deployment
	DeploymentReleaseNameAnnotation = "helm.deployment-orchestrator.io/release-name"
)

// setupHelmActionConfig creates and initializes Helm settings and action configuration
// This is a common pattern used across multiple Helm operations
func setupHelmActionConfig(ctx context.Context, namespace string) (*cli.EnvSettings, *action.Configuration, error) {
	log := logf.FromContext(ctx)

	settings := cli.New()
	actionConfig := &action.Configuration{}

	// Initialize the action configuration with Kubernetes client
	if err := actionConfig.Init(settings.RESTClientGetter(), namespace, "secrets", func(format string, v ...any) {
		log.Info(fmt.Sprintf(format, v...))
	}); err != nil {
		return nil, nil, fmt.Errorf("failed to initialize helm action configuration: %w", err)
	}

	return settings, actionConfig, nil
}

// getHelmRelease verifies if a Helm release exists and returns it
func getHelmRelease(ctx context.Context, component *deploymentsv1alpha1.Component) (*release.Release, error) {
	// Get release name from stored annotation
	releaseName := component.Annotations[DeploymentReleaseNameAnnotation]
	if releaseName == "" {
		return nil, fmt.Errorf("release name annotation %s not found", DeploymentReleaseNameAnnotation)
	}

	// Get target namespace from stored annotation
	targetNamespace := component.Annotations[DeploymentNamespaceAnnotation]
	if targetNamespace == "" {
		return nil, fmt.Errorf("target namespace annotation %s not found", DeploymentNamespaceAnnotation)
	}

	// Initialize Helm settings and action configuration
	_, actionConfig, err := setupHelmActionConfig(ctx, targetNamespace)
	if err != nil {
		return nil, err
	}

	// Get release status
	statusAction := action.NewStatus(actionConfig)
	rel, err := statusAction.Run(releaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to get release status: %w", err)
	}

	return rel, nil
}

// gatherHelmReleaseResources extracts Kubernetes resources from a Helm release manifest
// and builds a ResourceList for status checking
func gatherHelmReleaseResources(ctx context.Context, rel *release.Release) (kube.ResourceList, error) {
	log := logf.FromContext(ctx)

	if rel.Manifest == "" {
		log.Info("Release has no manifest, treating as ready")
		return kube.ResourceList{}, nil
	}

	// Initialize Helm settings and action configuration to get access to kube.Client
	_, actionConfig, err := setupHelmActionConfig(ctx, rel.Namespace)
	if err != nil {
		return nil, err
	}

	// Get the KubeClient from the action configuration
	kubeClient := actionConfig.KubeClient

	// Use Helm's Build function to parse the manifest into ResourceList
	resourceList, err := kubeClient.Build(bytes.NewBufferString(rel.Manifest), false)
	if err != nil {
		return nil, fmt.Errorf("failed to build resource list from manifest: %w", err)
	}

	log.Info("Built resource list from release manifest",
		"releaseName", rel.Name,
		"resourceCount", len(resourceList))

	return resourceList, nil
}

// extractReleaseInfo gets the release name and target namespace based on the operation mode
// For installs: generates release name and determines namespace from component/config
// For upgrades: extracts release name and namespace from component annotations
func extractReleaseInfo(component *deploymentsv1alpha1.Component, config *HelmConfig) (string, string, error) {
	if config != nil {
		// For installs: generate name and determine namespace
		releaseName := generateReleaseName(component)
		targetNamespace := component.Namespace
		if config.Namespace != "" {
			targetNamespace = config.Namespace
		}
		return releaseName, targetNamespace, nil
	} else {
		// For upgrades: get from annotations
		releaseName := component.Annotations[DeploymentReleaseNameAnnotation]
		if releaseName == "" {
			return "", "", fmt.Errorf("release name annotation %s not found - component may not have been properly deployed", DeploymentReleaseNameAnnotation)
		}
		targetNamespace := component.Annotations[DeploymentNamespaceAnnotation]
		if targetNamespace == "" {
			return "", "", fmt.Errorf("target namespace annotation %s not found - component may not have been properly deployed", DeploymentNamespaceAnnotation)
		}
		return releaseName, targetNamespace, nil
	}
}
