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

- **Bare Repository Caching**: Maintains shared bare repositories (git metadata only, no working directories) for efficient storage
- **Chart Extraction**: Uses `git archive` to extract charts to temporary directories without working directory conflicts
- **Concurrent Access**: Read-only git operations enable true parallel access across multiple Components
- **Network Optimization**: Bare repository sharing minimizes clone operations while supporting multiple refs

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

### Git Client

- Uses go-git library for repository operations
- Supports bare repository cloning and ref management
- Implements git archive extraction for chart access

### Caching Strategy

**Bare Repository Cache**:

- Location: `/helm/git-repos/<repo-hash>.git/`
- Sharing: Multiple Components accessing the same repository share the bare repo
- Locking: Repository-level locking only for clone/fetch operations

**Chart Extraction**:

- Per-reconcile extraction using `git archive` to temporary directories
- Automatic cleanup of temporary extraction directories
- Each Component gets isolated chart copy

**Cache Lifecycle**:

- Bare repositories persist across pod restarts (when using PVC)
- LRU eviction based on last access time for storage management
- Configurable TTL for unused repositories (default: 7 days)
- Reference-counted cleanup prevents deletion while in use

**Concurrent Access Pattern**:

```text
/helm/git-repos/
  github.com-org-charts-abc123.git/   # Bare repo (shared)
  
# Per-reconcile temporary extractions (auto-cleanup)
/tmp/
  component-foo-xyz123/               # Isolated extraction
    charts/webapp/
  component-bar-abc456/               # Concurrent extraction
    charts/database/
```

### Version Management

- Supports semantic versioning tags, branch names, and commit hash references
- Fetches required refs into bare repository on-demand with validation
- Efficient storage through git object deduplication across refs

### Ref Mutability Considerations

**Security Warning**: Git branches are mutable references. For production deployments:

- **Recommended**: Use immutable refs (tags or commit SHAs) for reproducibility
- **Discouraged**: Using branch names (e.g., `main`, `develop`) may cause deployment drift
- **Validation**: Component status should track resolved commit SHA for audit trail

## Security Considerations

- **Authentication**: Credential resolution from Kubernetes secrets with namespace scoping (Component namespace first, fallback to system namespace)
- **Immutable Refs**: Strongly recommend tags or commit SHAs over mutable branches in production (see Ref Mutability Considerations above)
- **Commit Tracking**: Track resolved commit SHA in Component status for reproducibility and audit trail
- **Isolation**: Bare repositories contain only git metadata; chart extractions use restricted temporary directories
- **Cache Management**: Automated cleanup of unused repositories prevents unbounded storage growth
