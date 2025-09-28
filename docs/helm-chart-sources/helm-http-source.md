# HTTP Repository Chart Source

## Overview

HTTP repositories represent the traditional chart distribution mechanism through web-based chart repositories. This source type supports both public and private repositories with comprehensive authentication options.

**Chart Addressing**: HTTP sources use separate repository and chart specification, allowing multiple charts to be served from a single repository endpoint.

## Configuration Schema

```yaml
source:
  type: http
  repository: https://charts.private-company.com/stable
  chart: webapp:1.2.3
  # Optional authentication for private repositories
  authentication:
    method: basic  # basic, bearer, client-cert, or custom
    secretRef:
      name: chart-repo-auth
      namespace: deployment-system
```

## Authentication Methods

- **Basic Authentication**: Username/password credentials for traditional HTTP authentication
- **Bearer Token**: API token authentication for modern chart repositories
- **TLS Client Certificates**: Mutual TLS authentication for high-security environments  
- **Custom Headers**: Support for proprietary authentication headers

## Operational Characteristics

- **Repository Indexing**: Leverages Helm's repository index mechanism for chart discovery
- **Caching Strategy**: Uses Helm's native repository and chart caching
- **Transport Security**: Enforces TLS with certificate validation
- **Error Handling**: Distinguishes authentication failures from network or chart resolution errors

## Configuration Structure

```go
type HTTPSource struct {
    Type           string              `json:"type" validate:"eq=http"`
    Repository     string              `json:"repository" validate:"required,url"`
    Chart          string              `json:"chart" validate:"required"`
    Authentication *HTTPAuthentication `json:"authentication,omitempty"`
}

type HTTPAuthentication struct {
    Method    string    `json:"method" validate:"required,oneof=basic bearer client-cert custom"`
    SecretRef SecretRef `json:"secretRef" validate:"required"`
}

type SecretRef struct {
    Name      string `json:"name" validate:"required"`
    Namespace string `json:"namespace" validate:"required"`
}
```

## Authentication Secret Schemas

Authentication secrets use specialized formats based on authentication method:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: http-credentials
data:
  # For method: basic
  username: <base64-encoded-username>
  password: <base64-encoded-password>
  # OR method: bearer
  token: <base64-encoded-token>
  # OR method: client-cert
  tls.crt: <base64-encoded-certificate>
  tls.key: <base64-encoded-private-key>
```

## Implementation Architecture

- **Transport Configuration**: Extends Helm's HTTP client configuration with authentication transport
- **Credential Management**: Secure resolution and injection of authentication credentials from Kubernetes secrets
- **Chart Loading**: Leverages existing `setupHelmRepository` and `loadHelmChart` functions with authentication extensions

## Security Considerations

- **Namespace Scoping**: Credentials resolved within Component namespace first, fallback to system namespace
- **TLS Enforcement**: Require secure transport for all external communications
- **Certificate Validation**: Implement proper certificate chain validation
- **Credential Rotation**: Handle credential rotation without service disruption
