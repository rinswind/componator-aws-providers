# OCI Registry Chart Source

## Overview

OCI registries enable chart distribution through container registries following OCI standards for artifact distribution. This approach aligns chart distribution with container image distribution patterns familiar to Kubernetes operators.

**Chart Addressing**: OCI sources use unified chart references that embed registry, repository path, chart name, and version in a single URL scheme.

## Configuration Schema

```yaml
source:
  type: oci
  chart: oci://ghcr.io/organization/charts/application:1.2.3
  # Optional authentication for private registries
  authentication:
    method: registry  # registry authentication type
    secretRef:
      name: registry-credentials
      namespace: deployment-system
```

## Authentication Methods

- **Registry Authentication**: Unified authentication method compatible with Docker registry authentication patterns
- **Credential Types**: Supports both username/password and token-based authentication
- **Registry Compatibility**: Works with standard OCI-compliant registries (Docker Hub, GitHub Container Registry, etc.)

## Operational Characteristics

- **Chart Distribution**: Charts are packaged and distributed as OCI artifacts alongside container images
- **Version Management**: Leverages OCI tag and digest mechanisms for precise version control
- **Caching Strategy**: Utilizes Helm's existing chart caching mechanisms optimized for OCI artifacts
- **Registry Integration**: Native integration with Helm's OCI support for seamless registry operations

## Configuration Structure

```go
type OCISource struct {
    Type           string             `json:"type" validate:"eq=oci"`
    Chart          string             `json:"chart" validate:"required,oci_reference"`
    Authentication *OCIAuthentication `json:"authentication,omitempty"`
}

type OCIAuthentication struct {
    Method    string    `json:"method" validate:"eq=registry"`
    SecretRef SecretRef `json:"secretRef" validate:"required"`
}

type SecretRef struct {
    Name      string `json:"name" validate:"required"`
    Namespace string `json:"namespace" validate:"required"`
}
```

## Authentication Secret Schema

Authentication secrets use registry-compatible format:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: registry-credentials
data:
  # For method: registry
  username: <base64-encoded-username>
  password: <base64-encoded-password>
  # OR
  token: <base64-encoded-token>
```

## Implementation Architecture

- **Registry Client**: Uses Helm's native OCI support for registry communication and authentication
- **Chart References**: Handles `oci://` URL schemes with embedded registry, chart, and version information
- **Credential Resolution**: Integrates with Kubernetes secrets for secure registry authentication
- **TLS Security**: Enforces certificate validation for registry connections with configurable policies

## Security Considerations

- **Credential Isolation**: Registry credentials must be resolved from secrets within the Component's namespace or designated system namespace
- **TLS Verification**: Enforce certificate validation for registry connections unless explicitly disabled through configuration
- **Access Control**: Support both username/password and token-based authentication methods
- **Namespace Scoping**: Credentials resolved within Component namespace first, fallback to system namespace
