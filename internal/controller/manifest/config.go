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

	"github.com/go-playground/validator/v10"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// manifestValidator is a package-level validator instance that is reused across all reconciliations.
// validator.Validator is thread-safe and designed for concurrent use, making this safe to share.
// Custom validators are registered in init() to ensure they're available before any validation occurs.
var manifestValidator = validator.New()

func init() {
	// Register custom manifest validator once at package initialization
	if err := manifestValidator.RegisterValidation("k8s_manifest", validateK8sManifest); err != nil {
		panic(fmt.Sprintf("failed to register manifest validator: %v", err))
	}
}

// validateK8sManifest validates that a manifest map contains required Kubernetes fields.
// Expected fields: apiVersion, kind, and metadata.name
func validateK8sManifest(fl validator.FieldLevel) bool {
	manifest, ok := fl.Field().Interface().(map[string]interface{})
	if !ok {
		return false
	}

	// Check for required apiVersion
	if apiVersion, ok := manifest["apiVersion"].(string); !ok || apiVersion == "" {
		return false
	}

	// Check for required kind
	if kind, ok := manifest["kind"].(string); !ok || kind == "" {
		return false
	}

	// Check for required metadata.name
	metadata, ok := manifest["metadata"].(map[string]interface{})
	if !ok {
		return false
	}

	if name, ok := metadata["name"].(string); !ok || name == "" {
		return false
	}

	return true
}

// ManifestConfig represents the configuration structure for Manifest components
// that gets unmarshaled from Component.Spec.Config.
type ManifestConfig struct {
	// Manifests contains an array of Kubernetes resource manifests to apply.
	// Each manifest is represented as a map[string]interface{} containing the
	// parsed YAML/JSON structure (apiVersion, kind, metadata, spec, etc.).
	Manifests []map[string]interface{} `json:"manifests" validate:"required,min=1,dive,k8s_manifest"`
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

	// Validate configuration using struct tags and custom validators
	if err := manifestValidator.Struct(&config); err != nil {
		return nil, fmt.Errorf("invalid manifest config: %w", err)
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
