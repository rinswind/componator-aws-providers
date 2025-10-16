// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// applyOptions returns the options for server-side apply operations.
func applyOptions(fieldManager string) metav1.ApplyOptions {
	return metav1.ApplyOptions{
		FieldManager: fieldManager,
		Force:        false, // Don't force - fail on conflicts
	}
}

// getOptions returns the options for Get operations.
func getOptions() metav1.GetOptions {
	return metav1.GetOptions{}
}

// deleteOptions returns the options for Delete operations.
func deleteOptions() metav1.DeleteOptions {
	return metav1.DeleteOptions{}
}

// resourceErrorf creates a formatted error message for a specific resource reference.
func resourceErrorf(ref ResourceReference, format string, args ...interface{}) error {
	// Prepend resource identification to the message
	msg := fmt.Sprintf(format, args...)
	if ref.Namespace != "" {
		return fmt.Errorf("resource %s %s/%s: %s", ref.Kind, ref.Namespace, ref.Name, msg)
	}
	return fmt.Errorf("resource %s %s: %s", ref.Kind, ref.Name, msg)
}
