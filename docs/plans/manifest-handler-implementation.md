# Implementation Plan: Manifest Component Handler

## Feature Overview

Implement a manifest Component handler that applies raw Kubernetes manifests (YAML resources) to the cluster using server-side apply and tracks their readiness using the kstatus library. This enables users to deploy platform configuration resources (ClusterIssuers, SecretStores, etc.) as Components within ComponentSets, eliminating the need for separate kubectl apply steps or pre-deployment setup.

## Architecture Impact

**Affected Components:**

- `deployment-operator-handlers`: New manifest handler package
- Component Handler Protocol: Follows standard claiming, creation, deletion patterns
- No changes to CRD or orchestrator logic required

**Key Integration Points:**

- Uses dynamic client for applying arbitrary resource types without pre-registration
- Leverages kstatus library for standardized readiness detection across CRD types
- Follows componentkit/controller base patterns for protocol compliance
- Integrates with dependency system for ordered deployment

**Design Trade-offs:**

- **Dynamic client vs cached client**: Dynamic client bypasses controller cache, enabling support for arbitrary CRDs without pre-registration. Trade-off: direct API server hits vs universal resource support. Decision: Use dynamic client since manifest operations are infrequent compared to core reconciliation.
- **Status checking strategy**: kstatus library vs custom logic. Decision: Use kstatus for battle-tested handling of standard conditions and observedGeneration patterns. Covers cert-manager, external-secrets, and most modern CRDs.
- **Manifest format**: Inline structured data vs raw YAML string vs external references. Decision: Start with inline structured data (parsed JSON) for type safety and validation. Can add YAML string support later if needed.

**Architecture Patterns:**

- Component Handler Protocol (claiming, creation, deletion)
- ComponentOperations factory pattern with pre-parsed configuration
- Dynamic client for runtime resource discovery
- kstatus for standardized readiness checking

## API Changes

**New Types:**

- `ManifestConfig`: Handler configuration structure
  - Fields: `manifests` (array of map[string]interface{})
- `ManifestStatus`: Handler status structure
  - Fields: `appliedResources` (array of ResourceReference)
- `ResourceReference`: Identifies an applied resource
  - Fields: `apiVersion` (string), `kind` (string), `name` (string), `namespace` (string, optional)
- `ManifestOperations`: ComponentOperations implementation
  - Implements: `Deploy`, `CheckDeployment`, `Delete`, `CheckDeletion`
- `ManifestOperationsFactory`: ComponentOperationsFactory implementation
  - Fields: `dynamicClient` (dynamic.Interface), `mapper` (meta.RESTMapper)

**New Functions:**

- `NewManifestOperationsFactory(dynamicClient, mapper) *ManifestOperationsFactory`: Creates factory with required clients
- `(f *ManifestOperationsFactory) NewOperations(ctx, config, status) (ComponentOperations, error)`: Parses config and creates operations instance

**Modified Types:**

None - uses existing Component CRD and componentkit interfaces.

## Critical Logic and Decisions

### Component Configuration

```yaml
# Component spec.config
config:
  manifests:
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

### Deployment Flow

**Deploy Operation:**

```text
for each manifest in config.manifests:
  convert manifest map to unstructured.Unstructured
  
  add tracking label:
    "manifest.deployment-orchestrator.io/component" = component.Name
  
  determine GVR from GVK using RESTMapper
  
  apply using dynamic client with server-side apply:
    dynamicClient.Resource(gvr).Apply(ctx, obj, 
      FieldOwner="manifest-handler",
      Force=false)
  
  if error:
    return Failed with error message
  
  record applied resource reference

persist applied resource list in handler status
return Success=false (not ready yet, needs status check)
```

**Status Checking Flow:**

```text
for each applied resource reference:
  get current resource from API server
  
  if not found:
    return Failed "resource disappeared"
  
  compute status using kstatus.Compute(resource)
  
  if status == Failed:
    return Failed with kstatus message
  
  if status == InProgress or Unknown:
    return Success=false (still progressing)
  
  if status == Current:
    continue to next resource

if all resources Current:
  return Success=true (ready)
```

**Deletion Flow:**

```text
retrieve applied resource list from handler status

for each resource in reverse order:
  determine GVR from resource reference
  
  delete using dynamic client:
    dynamicClient.Resource(gvr).Delete(ctx, name, namespace)
  
  if error and not IsNotFound:
    log warning but continue (best effort cleanup)

return Success=true (deletion complete)
```

**Design Decisions:**

- **Server-side apply**: Uses Kubernetes server-side apply (SSA) with field manager "manifest-handler" for proper field ownership tracking and conflict detection. SSA provides idempotent operations and is the modern standard for declarative resource management.
- **Resource tracking**: Store applied resource references in handler status to enable proper cleanup during deletion. Handles cluster-scoped resources correctly.
- **Deletion order**: Reverse order application helps with dependencies (e.g., delete dependent resources before ClusterIssuer).
- **Error handling**: Apply errors are fatal (transition to Failed). Deletion errors are logged but don't block finalizer removal (best-effort cleanup).
- **Configuration changes**: Orchestrator triggers re-apply on config changes via generation/observedGeneration mechanism. Server-side apply safely handles updates.

### kstatus Integration

```text
import "sigs.k8s.io/cli-utils/pkg/kstatus/status"

statusResult, err := status.Compute(unstructuredObj)

map kstatus result to operation result:
  status.CurrentStatus     → Success=true (ready)
  status.InProgressStatus  → Success=false (still deploying)
  status.FailedStatus      → Failed phase with message
  status.UnknownStatus     → Success=false (keep checking)
  status.NotFoundStatus    → Failed "resource not found"
  status.TerminatingStatus → Success=false (wait for deletion)
```

**What kstatus provides:**

- Standard conditions checking (Ready, Available, etc.)
- observedGeneration vs generation comparison
- Built-in logic for ~20 resource types
- Fallback to conditions for CRDs
- Handles cert-manager, external-secrets, and most modern operators

### Handler Status Structure

```go
type ManifestStatus struct {
    AppliedResources []ResourceReference `json:"appliedResources"`
}

type ResourceReference struct {
    APIVersion string `json:"apiVersion"`
    Kind       string `json:"kind"`
    Name       string `json:"name"`
    Namespace  string `json:"namespace,omitempty"`
}
```

**Rationale**: Applied resources list enables proper cleanup during deletion. Hash tracking removed as unnecessary - orchestrator handles config changes via generation mechanism, and server-side apply provides idempotency.

## Testing Approach

**Unit Tests:**

- Config parsing (valid and invalid configurations)
- Manifest to unstructured conversion
- Resource reference generation
- kstatus result mapping

**Integration Tests (envtest):**

- Apply single cluster-scoped resource (ConfigMap)
- Apply single namespaced resource (Secret)
- Apply multiple resources and check readiness
- Handle resource with Ready condition
- Handle resource without conditions
- Deletion removes all applied resources
- Protocol compliance (claiming, finalizer management)
- Configuration changes trigger re-application

**Critical Scenarios:**

- ClusterIssuer application and readiness (primary use case)
- SecretStore application and readiness
- Mixed cluster and namespace scoped resources
- Resource that transitions Ready → NotReady
- Deletion with missing resources (cleanup resilience)

## Implementation Phases

### Phase 1: Core Handler Structure

- Create handler package structure following helm/rds patterns
- Implement ComponentReconciler with componentkit base
- Implement ManifestOperationsFactory with dynamic client injection
- Wire up RBAC and controller registration in main.go
- Deliverable: Handler compiles and registers, filters Components correctly

### Phase 2: Manifest Application

- Implement config parsing (ManifestConfig structure)
- Implement Deploy operation with server-side apply
- Track applied resources in handler status
- Handle GVR resolution and namespace/cluster scope
- Deliverable: Can apply manifests and persist resource references

### Phase 3: Status Checking with kstatus

- Add kstatus library dependency
- Implement CheckDeployment with kstatus.Compute
- Map kstatus results to ComponentPhase
- Handle missing resources and status transitions
- Deliverable: Correctly detects readiness for cert-manager, ESO resources

### Phase 4: Deletion and Cleanup

- Implement Delete operation with dynamic client
- Delete resources in reverse order
- Handle best-effort cleanup (ignore NotFound)
- Remove finalizer on completion
- Deliverable: Clean resource deletion, protocol compliant

### Phase 5: Testing and Documentation

- Add unit tests for config parsing and status mapping
- Add integration tests for apply, status check, delete
- Test with ClusterIssuer and SecretStore examples
- Document configuration format and limitations
- Deliverable: Tested, documented, ready for use

## Open Questions

None - architecture research phase completed. Implementation can proceed with:

- Dynamic client for arbitrary resource support
- kstatus library for standardized readiness checking
- Server-side apply for idempotent operations
- ComponentOperations pattern following existing handlers
