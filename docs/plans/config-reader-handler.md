# Implementation Plan: Config-Reader Component Handler

## Feature Overview

Implement a config-reader Component handler that reads ConfigMaps from arbitrary namespaces
and exports selected values in Component handlerStatus. This enables Compositions to
bootstrap from external configuration (e.g., Terraform outputs) by referencing
config-reader Components as dependencies. The handler watches ConfigMaps for changes and
automatically propagates updates through the orchestration system via status updates.

## Architecture Impact

**Affected Components:**

- `deployment-operator-handlers`: New config-reader handler package
- Configuration Resolution Protocol: Existing protocol supports this use case
- Orchestration cascade: Status changes trigger template re-resolution

**Key Integration Points:**

- Watches ConfigMaps cluster-wide using componentkit builder customization
- Uses APIReader for non-cached ConfigMap access to avoid memory explosion at scale
- Updates handlerStatus which triggers orchestrator reconciliation
- Orchestrator re-resolves dependent component templates with new values
- Dependent components get updated/recreated automatically

**Scale Considerations:**

- Single handler deployment serves thousands of Components
- ConfigMap cache shared across all Components (efficient for common ConfigMaps)
- Watch mapper filters to only reconcile affected Components
- 10k Components × 2 ConfigMaps → ~2k unique ConfigMaps cached (~100MB)

**Architecture Patterns:**

- Component Handler Protocol (claiming, creation, deletion)
- Configuration Resolution Protocol (provides values for template references)
- Watch-based change propagation (ConfigMap change → status update → orchestrator cascade)

## API Changes

**New Types:**

- `ConfigReaderConfig`: Handler configuration structure
  - Fields: `sources` (array of `ConfigMapSource`)
- `ConfigMapSource`: Single ConfigMap source definition
  - Fields: `name` (string), `namespace` (string), `exports` (array of `ExportMapping`)
- `ExportMapping`: Key export mapping
  - Fields: `key` (string), `as` (string, optional, defaults to `key`)
- `ConfigReaderStatus`: Handler status structure
  - Fields: map[string]string of exported values
- `ConfigReaderOperations`: ComponentOperations implementation
  - Implements: `Deploy`, `CheckDeployment`, `Delete`, `CheckDeletion`
- `ConfigReaderOperationsFactory`: ComponentOperationsFactory implementation
  - Parses config once, creates operations instances

**Modified Types:**

None - uses existing Component CRD and componentkit interfaces.

## Critical Logic and Decisions

### Configuration Structure

```yaml
# Component spec.config
config:
  sources:
    - name: terraform-outputs
      namespace: default
      exports:
        - key: eso_irsa_role_arn
        - key: vpc_id
        - key: eks_cluster_endpoint
          as: cluster_endpoint  # Rename for brevity
    - name: shared-config
      namespace: kube-system
      exports:
        - key: cluster_domain
```

**Design Decisions:**

- `sources` wrapper provides clear structure and extensibility
- Export mapping with optional `as` for renaming - flexible but simple
- Flat handlerStatus output - all exports in single map

### ComponentOperations Implementation

**Deploy Operation:**

```text
Deploy():
  for each source in config.sources:
    fetch ConfigMap using APIReader (bypass cache)
    if not found:
      return error (permanent failure)
    
    for each export in source.exports:
      extract value from ConfigMap.data[export.key]
      outputKey = export.as if set, else export.key
      add to handlerStatus map
  
  return OperationResult:
    UpdatedStatus: handlerStatus as JSON
    Success: true
```

**CheckDeployment Operation:**

```text
CheckDeployment():
  # Config-reader has no async operations
  # Deploy completes immediately
  return OperationResult:
    Success: true
```

**Delete Operation:**

```text
Delete():
  # No resources to clean up
  return OperationResult:
    Success: true
```

**CheckDeletion Operation:**

```text
CheckDeletion():
  # Deletion completes immediately
  return OperationResult:
    Success: true
```

**Design Decisions:**

- Deploy reads and returns immediately - no async operations
- Missing ConfigMaps are permanent errors - fail Component
- Missing keys in ConfigMap are permanent errors - fail Component
- No cleanup needed - config-reader creates no resources
- Use APIReader in Deploy to avoid cache dependency

### Watch Mechanism and Cascade

**SetupWithManager customization:**

```go
func (r *ConfigReaderReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return r.ComponentReconciler.NewDefaultController(mgr).
        Watches(
            &corev1.ConfigMap{},
            handler.EnqueueRequestsFromMapFunc(r.mapConfigMapToComponents),
        ).
        Complete(r.ComponentReconciler)
}
```

**ConfigMap to Component Mapper:**

```text
mapConfigMapToComponents(configMap):
  list all Components with handler="config-reader"
  
  for each component:
    parse component.spec.config
    
    for each source in config.sources:
      if source references this configMap:
        add component to reconcile list
  
  return reconcile requests
```

**Cascade Flow:**

```text
1. ConfigMap changes
2. Watch event triggers config-reader reconcile
3. Deploy reads new ConfigMap values via APIReader
4. handlerStatus updated with new values
5. Component status change triggers orchestrator watch
6. Orchestrator re-resolves dependent component templates
7. Detects drift in dependent components
8. Deletes and recreates dependents with new values
9. Changes propagate through dependency chain
```

**Design Decisions:**

- Watch all ConfigMaps - single handler can efficiently filter
- Mapper parses Component configs to find affected ones
- Use APIReader in Deploy for fresh values - don't rely on watch cache
- Status update triggers orchestrator - existing protocol
- No polling needed - watch provides immediate notification

### Error Handling

**Configuration Errors:**

- Invalid config JSON → permanent failure, transition to Failed
- Missing required fields → permanent failure, transition to Failed

**Runtime Errors:**

- ConfigMap not found → permanent failure, detailed error message
- ConfigMap key not found → permanent failure, list available keys
- Permission denied → transient failure, requeue (may be RBAC propagation)

**Design Decisions:**

- Config and key errors are permanent - user must fix Component spec
- Permission errors retry - RBAC may be eventually consistent
- Clear error messages guide user to resolution

## Testing Approach

**Unit Tests:**

- Config parsing and validation
- Export mapping logic (key extraction, renaming)
- Error handling for missing ConfigMaps/keys

**Integration Tests:**

- Component creation with valid ConfigMap references
- handlerStatus correctly populated from ConfigMap values
- ConfigMap changes trigger Component reconciliation
- Dependent Components get updated values via orchestrator cascade
- Multiple Components sharing same ConfigMap
- Component with ConfigMap in different namespace

**Critical Scenarios:**

- Basic config extraction and status population
- ConfigMap watch and cascade to dependents
- Missing ConfigMap error handling
- Missing key error handling
- Multi-component ConfigMap sharing
- Cross-namespace ConfigMap access

## Implementation Phases

### Phase 1: Core Handler Implementation

- Create config-reader package structure in deployment-operator-handlers
- Implement config types (ConfigReaderConfig, ConfigMapSource, ExportMapping)
- Implement operations factory and operations (Deploy, Delete, Check methods)
- Use APIReader for ConfigMap fetching in Deploy
- Add RBAC annotations for ConfigMap access
- Write unit tests for config parsing and export logic

**Validation:** Handler compiles, unit tests pass, config parsing works

### Phase 2: Watch Integration and Controller Setup

- Implement ComponentReconciler with builder customization
- Add ConfigMap watch using Watches() in SetupWithManager
- Implement mapConfigMapToComponents mapper function
- Add controller to main.go registration
- Test watch triggers reconciliation for affected Components

**Validation:** ConfigMap changes trigger reconciles, mapper filters correctly

### Phase 3: Integration Testing and Documentation

- Write integration tests for full cascade (ConfigMap → status → orchestrator → dependents)
- Test multi-component scenarios and cross-namespace access
- Add handler README with config examples and architecture
- Document use case in architecture docs (bootstrapping from external config)
- Test at scale (simulate hundreds of Components)

**Validation:** Integration tests pass, documentation complete, scale verified

## Open Questions

None - design is well-defined and aligns with existing architecture patterns.
