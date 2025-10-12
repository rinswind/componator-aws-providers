// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package http

// Config represents HTTP repository configuration parsed from Component spec.
// This matches the existing JSON schema for backward compatibility.
type Config struct {
	// Repository specifies the Helm chart repository configuration
	Repository RepositoryConfig `json:"repository" validate:"required"`

	// Chart specifies the chart name and version to deploy
	Chart ChartConfig `json:"chart" validate:"required"`

	// Authentication contains optional HTTP repository authentication
	// +optional
	Authentication *AuthenticationConfig `json:"authentication,omitempty"`
}

// RepositoryConfig represents Helm chart repository configuration.
type RepositoryConfig struct {
	// URL is the chart repository URL
	URL string `json:"url" validate:"required,url"`

	// Name is the repository name for local reference
	Name string `json:"name" validate:"required,min=1"`
}

// ChartConfig represents chart identification and version specification.
type ChartConfig struct {
	// Name is the chart name within the repository
	Name string `json:"name" validate:"required,min=1"`

	// Version specifies the chart version to install
	Version string `json:"version" validate:"required,min=1"`
}

// AuthenticationConfig contains authentication configuration for HTTP repositories.
// Currently reserved for future implementation.
type AuthenticationConfig struct {
	// Future: username/password, token-based auth
}
