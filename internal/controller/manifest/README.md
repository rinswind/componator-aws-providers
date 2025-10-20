# Manifest Component Handler

## Overview

The Manifest handler applies raw Kubernetes manifests (YAML resources) to the cluster using server-side apply and tracks their readiness using the kstatus library. This enables deploying platform configuration resources (ClusterIssuers, SecretStores, etc.) as Components within ComponentSets.

## Features

- **Universal Resource Support**: Apply any Kubernetes resource type using dynamic client
- **Server-Side Apply**: Idempotent operations with proper field ownership tracking
- **Smart Readiness Detection**: Uses kstatus library for standardized status checking across CRD types
- **Dependency Ordering**: Apply resources in order, delete in reverse order
- **Best-Effort Cleanup**: Continue deletion even if some resources fail to clean up

## Configuration

The handler accepts a `manifests` array containing Kubernetes resource definitions:

```yaml
apiVersion: deployment-orchestrator.io/v1alpha1
kind: Component
metadata:
  name: platform-config
spec:
  handler: manifest
  config:
    manifests:
      # cert-manager ClusterIssuer
      - apiVersion: cert-manager.io/v1
        kind: ClusterIssuer
        metadata:
          name: letsencrypt-prod
        spec:
          acme:
            server: https://acme-v02.api.letsencrypt.org/directory
            email: admin@example.com
            privateKeySecretRef:
              name: letsencrypt-prod-key
            solvers:
              - http01:
                  ingress:
                    class: nginx
      
      # external-secrets SecretStore
      - apiVersion: external-secrets.io/v1beta1
        kind: SecretStore
        metadata:
          name: aws-secrets
          namespace: default
        spec:
          provider:
            aws:
              service: SecretsManager
              region: us-east-1
```

### Configuration Fields

- `manifests` (required): Array of Kubernetes resource manifests
  - Each manifest must have `apiVersion`, `kind`, `metadata.name`
  - Namespaced resources must have `metadata.namespace`

## How It Works

### Deployment Flow

1. **Apply Phase**:
   - Converts each manifest to unstructured object
   - Resolves GVK to GVR using REST mapper
   - Applies using server-side apply with field manager "manifest-handler"
   - Records applied resource references in handler status

2. **Status Checking Phase**:
   - Retrieves each applied resource from API server
   - Computes status using kstatus library
   - Maps kstatus results:
     - `CurrentStatus` → Ready
     - `InProgressStatus` → Still deploying
     - `FailedStatus` → Failed with error message
     - `UnknownStatus` → Keep checking
     - `TerminatingStatus` → Failed (unexpected)
     - `NotFoundStatus` → Failed (resource disappeared)

### Deletion Flow

1. **Delete Phase**:
   - Deletes resources in reverse order (helps with dependencies)
   - Uses best-effort cleanup (continues on errors)
   - Logs warnings for resources that can't be deleted

2. **Verification Phase**:
   - Checks if any resources still exist
   - Returns success when all resources are deleted

## Status Tracking

The handler maintains status in `Component.Status.ProviderStatus`:

```json
{
  "appliedResources": [
    {
      "apiVersion": "cert-manager.io/v1",
      "kind": "ClusterIssuer",
      "name": "letsencrypt-prod",
      "namespace": ""
    },
    {
      "apiVersion": "external-secrets.io/v1beta1",
      "kind": "SecretStore",
      "name": "aws-secrets",
      "namespace": "default"
    }
  ]
}
```

This enables proper cleanup during deletion.

## Use Cases

- **Platform Configuration**: Deploy ClusterIssuers, SecretStores, custom resources
- **CRD Instances**: Create instances of custom resources
- **Namespace Configuration**: Set up RoleBindings, NetworkPolicies, etc.
- **Cross-Namespace Resources**: Deploy cluster-scoped resources that affect multiple namespaces

## Limitations

- **No Templating**: Manifests are applied as-is (use helm handler for templating)
- **No Rollback**: Failed applications remain in Failed state (manual cleanup required)
- **No Drift Detection**: Changes outside the handler are not detected
- **Resource Updates**: Configuration changes trigger re-application (server-side apply handles conflicts)

## Dependencies

- `k8s.io/client-go/dynamic`: Dynamic client for arbitrary resource types
- `sigs.k8s.io/cli-utils/pkg/kstatus`: Standardized status computation
- `k8s.io/apimachinery/pkg/api/meta`: GVK to GVR conversion

## Architecture Compliance

Follows the standard Component Handler protocol:

- ✅ Claiming Protocol: Uses handler-specific finalizer
- ✅ Creation Protocol: Immediate resource creation, status-driven progression
- ✅ Deletion Protocol: Finalizer-based deletion coordination
- ✅ Status Management: ComponentPhase enum with proper conditions

## Testing

Integration tests validate:

- Single cluster-scoped resource application
- Single namespaced resource application
- Multiple resource application and readiness
- Resources with Ready conditions
- Resources without conditions
- Deletion removes all resources
- Configuration changes trigger re-application
- Protocol compliance (claiming, finalizer management)
