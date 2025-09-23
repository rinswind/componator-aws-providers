# Helm Controller

This package contains the Kubernetes controller responsible for handling `Component` resources with `spec.handler: "helm"`.

## Purpose

The Helm controller manages the deployment and lifecycle of Helm charts based on Component specifications. It implements the component handler interface for Helm-based deployments.

## Controller Logic

- **Filtering**: Only processes Components where `spec.handler == "helm"`
- **Claiming**: Implements the claiming protocol to ensure exclusive ownership
- **Deployment**: Manages Helm chart installations and updates
- **Status**: Reports deployment status back to the Component resource

## Configuration

Component configuration for Helm deployments is passed through the `spec.config` field and typically includes:

- **Chart**: Helm chart reference (repository, name, version)
- **Values**: Custom values to override chart defaults
- **Namespace**: Target namespace for deployment
- **Release Name**: Helm release name

## Dependencies

- `helm.sh/helm/v3` - Helm client library for chart operations
- `sigs.k8s.io/controller-runtime` - Controller framework
- Component CRD from `deployment-operator`
