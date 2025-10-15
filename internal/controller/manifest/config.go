// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

// config.go contains Manifest configuration parsing and validation logic.
// This includes the ManifestConfig struct definition and related parsing functions
// that handle Component.Spec.Config unmarshaling for manifest components.

package manifest

import (
	"context"
	"encoding/json"
	"fmt"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// ManifestConfig represents the configuration structure for Manifest components
// that gets unmarshaled from Component.Spec.Config.
type ManifestConfig struct {
	// Manifests contains an array of Kubernetes resource manifests to apply.
	// Each manifest is represented as a map[string]interface{} containing the
	// parsed YAML/JSON structure (apiVersion, kind, metadata, spec, etc.).
	Manifests []map[string]interface{} `json:"manifests" validate:"required,min=1,dive"`
}

// ManifestStatus contains handler-specific status data for Manifest deployments.
// This data is persisted across reconciliation loops in Component.Status.HandlerStatus.
type ManifestStatus struct {
	// AppliedResources tracks the resources that have been applied to the cluster.
	// This list is used during deletion to clean up all created resources.
	AppliedResources []ResourceReference `json:"appliedResources,omitempty"`
}

// ResourceReference identifies a specific Kubernetes resource.
type ResourceReference struct {
	// APIVersion is the API version of the resource (e.g., "v1", "cert-manager.io/v1")
	APIVersion string `json:"apiVersion"`

	// Kind is the kind of the resource (e.g., "Secret", "ClusterIssuer")
	Kind string `json:"kind"`

	// Name is the name of the resource
	Name string `json:"name"`

	// Namespace is the namespace of the resource (empty for cluster-scoped resources)
	Namespace string `json:"namespace,omitempty"`
}

// resolveManifestConfig unmarshals Component.Spec.Config into ManifestConfig struct.
func resolveManifestConfig(ctx context.Context, rawConfig json.RawMessage) (*ManifestConfig, error) {
	var config ManifestConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to parse manifest config: %w", err)
	}

	// Validate that manifests array is not empty
	if len(config.Manifests) == 0 {
		return nil, fmt.Errorf("manifests array cannot be empty")
	}

	log := logf.FromContext(ctx).WithValues("manifestCount", len(config.Manifests))
	log.V(1).Info("Resolved manifest config")

	return &config, nil
}

// resolveManifestStatus unmarshals Component.Status.HandlerStatus into ManifestStatus struct.
func resolveManifestStatus(ctx context.Context, rawStatus json.RawMessage) (*ManifestStatus, error) {
	status := &ManifestStatus{}
	if len(rawStatus) == 0 {
		logf.FromContext(ctx).V(1).Info("No existing manifest status found, starting with empty status")
		return status, nil
	}

	if err := json.Unmarshal(rawStatus, &status); err != nil {
		return nil, fmt.Errorf("failed to parse manifest status: %w", err)
	}

	return status, nil
}
