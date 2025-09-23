# Helm Controller

This package contains the Kubernetes controller responsible for handling `Component` resources with `spec.handler: "helm"`.

## Purpose

The Helm controller manages the deployment and lifecycle of Helm charts based on Component specifications. It implements the component handler interface for Helm-based deployments.

## Controller Logic

- **Filtering**: Only processes Components where `spec.handler == "helm"`
- **Claiming**: Implements the claiming protocol to ensure exclusive ownership
- **Deployment**: Manages Helm chart installations and updates
- **Status**: Reports deployment status back to the Component resource

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
    "service.type": "LoadBalancer",
    "replicaCount": "3"
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

- **values**: Key-value pairs for chart values override (all values must be strings)
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
    "auth.postgresPassword": "mysecretpassword",
    "auth.database": "myapp",
    "persistence.size": "20Gi"
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
    "controller.service.type": "LoadBalancer"
  }
}
```

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
- Component CRD from `deployment-operator`
