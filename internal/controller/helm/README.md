# Helm Controller

This package contains the Kubernetes controller responsible for handling `Component` resources with `spec.handler: "helm"`.

## Purpose

The Helm controller manages the deployment and lifecycle of Helm charts based on Component specifications. It implements the component handler interface for Helm-based deployments.

## Architecture

The Helm controller is organized into several focused modules:

- **`controller.go`** - Main reconciliation logic and Component lifecycle management
- **`config.go`** - Configuration parsing and validation (HelmConfig struct and methods)
- **`operations_deploy.go`** - Helm chart installation and deployment operations
- **`operations_delete.go`** - Helm chart uninstallation and cleanup operations  
- **`operations_utils.go`** - Shared utilities, constants, and helper functions
- **`*_test.go`** - Comprehensive test suites for all functionality

## Controller Logic

- **Filtering**: Only processes Components where `spec.handler == "helm"`
- **Claiming**: Implements the claiming protocol to ensure exclusive ownership
- **Deployment**: Manages Helm chart installations and updates through async operations
- **Status**: Reports deployment status back to the Component resource with detailed conditions
- **Cleanup**: Handles proper Helm release deletion and resource cleanup

## Configuration Schema

Component configuration for Helm deployments is passed through the `spec.config` field with the following structure:

```json
{
  "repository": {
    "url": "https://charts.bitnami.com/bitnami",
    "name": "bitnami"
  },
  "chart": {
    "name": "nginx",
    "version": "15.4.4"
  },
  "values": {
    "service": {
      "type": "LoadBalancer"
    },
    "replicaCount": 3
  },
  "namespace": "web"
}
```

### Required Fields

- **repository.url**: Chart repository URL (must be valid HTTP/HTTPS URL)
- **repository.name**: Repository name for local reference
- **chart.name**: Chart name within the repository
- **chart.version**: Chart version to install

### Optional Fields

- **values**: Nested JSON object for chart values override (supports any JSON structure: strings, numbers, booleans, objects, arrays)
- **namespace**: Target namespace for chart deployment (defaults to Component namespace)

### Configuration Examples

**Minimal Configuration**:

```json
{
  "repository": {
    "url": "https://charts.bitnami.com/bitnami",
    "name": "bitnami"
  },
  "chart": {
    "name": "nginx",
    "version": "15.4.4"
  }
}
```

**Configuration with Values Override**:

```json
{
  "repository": {
    "url": "https://charts.bitnami.com/bitnami",
    "name": "bitnami"
  },
  "chart": {
    "name": "postgresql",
    "version": "12.12.10"
  },
  "values": {
    "auth": {
      "postgresPassword": "mysecretpassword",
      "database": "myapp"
    },
    "persistence": {
      "size": "20Gi"
    }
  },
  "namespace": "database"
}
```

**Configuration for Different Repository**:

```json
{
  "repository": {
    "url": "https://kubernetes.github.io/ingress-nginx",
    "name": "ingress-nginx"
  },
  "chart": {
    "name": "ingress-nginx",
    "version": "4.8.3"
  },
  "values": {
    "controller": {
      "service": {
        "type": "LoadBalancer"
      }
    }
  }
}
```

## Deployment Annotations

The controller stores deployment metadata in Component annotations for tracking and cleanup:

- **`helm.deployment-orchestrator.io/target-namespace`** - Actual namespace where Helm release was deployed
- **`helm.deployment-orchestrator.io/release-name`** - Actual release name used for Helm deployment

These annotations enable proper cleanup operations and state tracking across reconciliation cycles.

## Release Naming

Helm releases are automatically named using the pattern: `{namespace}-{component-name}`

This ensures:

- **Uniqueness**: Same component name in different namespaces get different releases
- **Deterministic**: Same component always gets the same release name
- **Traceable**: Easy to identify which Component created which release

Example: Component named `web-frontend` in namespace `production` creates release `production-web-frontend`

## Dependencies

- `helm.sh/helm/v3` - Helm client library for chart operations
- `sigs.k8s.io/controller-runtime` - Controller framework
- `github.com/go-playground/validator/v10` - Configuration validation
- Component CRD from `deployment-operator` project

## Implementation Details

The controller implements the three core protocols required for Component handlers:

1. **Claiming Protocol** - Uses handler-specific finalizers for atomic resource discovery
2. **Creation Protocol** - Immediate resource creation with status-driven progression  
3. **Deletion Protocol** - Finalizer-based deletion coordination with proper cleanup

All Helm operations are designed to be non-blocking and idempotent, with comprehensive status reporting and error handling.
