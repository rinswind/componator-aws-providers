# Git Repository Chart Source

## Overview

Git repositories support chart development workflows where charts are maintained in version control alongside application code. This source type enables direct access to charts from Git repositories with flexible version referencing.

**Chart Addressing**: Git sources specify repository URL, chart path within the repository, and version reference (tag, branch, or commit).

## Configuration Schema

```yaml
source:
  type: git
  repository: https://github.com/organization/helm-charts.git
  path: charts/webapp
  ref: v1.2.3
  # Optional authentication for private repositories
  authentication:
    method: https  # https, ssh, or token
    secretRef:
      name: git-credentials
      namespace: deployment-system
```

## Authentication Methods

- **HTTPS Authentication**: Username/password credentials for HTTPS Git access
- **SSH Authentication**: SSH private key for Git over SSH protocol
- **Token Authentication**: GitHub/GitLab personal access tokens for API-based access

## Operational Characteristics

- **Repository Cloning**: Performs git repository cloning with authentication support
- **Chart Discovery**: Navigates repository directory structure to locate charts at specified paths
- **Version Resolution**: Supports semantic versioning tags, branch names, and commit hash references
- **Local Caching**: Maintains local repository cache with lifecycle-aligned cleanup

## Configuration Structure

```go
type GitSource struct {
    Type           string             `json:"type" validate:"eq=git"`
    Repository     string             `json:"repository" validate:"required,url"`
    Path           string             `json:"path" validate:"required"`
    Ref            string             `json:"ref" validate:"required"`
    Authentication *GitAuthentication `json:"authentication,omitempty"`
}

type GitAuthentication struct {
    Method    string    `json:"method" validate:"required,oneof=https ssh token"`
    SecretRef SecretRef `json:"secretRef" validate:"required"`
}

type SecretRef struct {
    Name      string `json:"name" validate:"required"`
    Namespace string `json:"namespace" validate:"required"`
}
```

## Authentication Secret Schemas

Authentication secrets use method-specific formats:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: git-credentials
data:
  # For method: https
  username: <base64-encoded-username>
  password: <base64-encoded-password>
  # OR method: ssh
  ssh-private-key: <base64-encoded-ssh-key>
  # OR method: token
  token: <base64-encoded-token>
```

## Implementation Architecture

- **Git Client**: Uses go-git library for repository operations and authentication
- **Caching Strategy**: Implements repository cache with concurrent access control and proper locking
- **Version Management**: Handles branch, tag, and commit hash resolution with validation
- **Network Optimization**: Supports shallow clones to minimize network usage and improve performance

## Security Considerations

- **Authentication Handling**: Implement credential resolution from Kubernetes secrets for private repository access
- **Version Validation**: Support both semantic versioning tags and branch/commit references
- **Local Caching**: Maintain local repository cache with proper cleanup lifecycle
- **Concurrent Access**: Handle multiple Components accessing the same repository through proper locking mechanisms
- **Namespace Scoping**: Credentials resolved within Component namespace first, fallback to system namespace
