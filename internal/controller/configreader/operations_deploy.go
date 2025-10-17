// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package configreader

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/rinswind/deployment-operator/componentkit/controller"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Deploy reads all ConfigMaps specified in config and exports values to handlerStatus.
// This operation completes synchronously - there are no async operations for config-reader.
func (o *ConfigReaderOperations) Deploy(ctx context.Context) (*controller.ActionResult, error) {
	log := logf.FromContext(ctx)

	// Reset status to start fresh
	o.status = make(ConfigReaderStatus)

	// Process each ConfigMap source
	for _, source := range o.config.Sources {
		log.V(1).Info("Reading ConfigMap",
			"name", source.Name,
			"namespace", source.Namespace,
			"exportCount", len(source.Exports))

		// Fetch ConfigMap using APIReader (bypass cache for fresh data)
		var configMap corev1.ConfigMap
		namespacedName := types.NamespacedName{
			Name:      source.Name,
			Namespace: source.Namespace,
		}

		if err := o.apiReader.Get(ctx, namespacedName, &configMap); err != nil {
			return controller.ActionResultForError(
				o.status, fmt.Errorf("failed to read ConfigMap %s/%s: %w", source.Namespace, source.Name, err),
				controller.IsRetryableKubernetesError)
		}

		// Extract and export each key
		for _, export := range source.Exports {
			value, exists := configMap.Data[export.Key]
			if !exists {
				// List available keys to help user debug
				availableKeys := strings.Join(slices.Sorted(maps.Keys(configMap.Data)), ", ")
				return controller.ActionResultForError(
					o.status,
					fmt.Errorf("key %q not found in ConfigMap %s/%s (available keys: %s)",
						export.Key, source.Namespace, source.Name, availableKeys),
					controller.IsRetryableKubernetesError)
			}

			// Determine output key (use 'as' if specified, otherwise use 'key')
			outputKey := export.Key
			if export.As != "" {
				outputKey = export.As
			}

			o.status[outputKey] = value
			log.V(1).Info("Exported ConfigMap value",
				"sourceKey", export.Key,
				"outputKey", outputKey,
				"configMap", fmt.Sprintf("%s/%s", source.Namespace, source.Name))
		}
	}

	log.Info("Successfully read all ConfigMaps", "exportCount", len(o.status))

	return controller.ActionSuccess(o.status)
}

// CheckDeployment always returns success immediately since Deploy completes synchronously.
// Config-reader has no async operations to wait for.
func (o *ConfigReaderOperations) CheckDeployment(ctx context.Context) (*controller.CheckResult, error) {
	return controller.CheckComplete(o.status)
}
