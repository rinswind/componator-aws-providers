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
	// Parse configuration to get release name and namespace
	config, err := resolveHelmConfig(component)
	if err != nil {
		return nil, fmt.Errorf("failed to parse helm config: %w", err)
	}

	releaseName := config.ReleaseName
	targetNamespace := config.Namespace

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
