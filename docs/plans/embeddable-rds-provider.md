# Implementation Plan: Embeddable RDS Provider

## Feature Overview

Make the RDS component provider embeddable in setkit distributions by moving it from `internal/controller/rds/` to a public `rds/` package with namespace support and a `Register()` function. This enables setkits to embed the RDS provider for deploying AWS RDS instances while preventing conflicts when multiple setkits coexist.

## Architecture Impact

**Architectural Patterns:**

- Component provider claiming protocol (handler-specific finalizers)
- Provider namespacing pattern: `{setkit}.{provider}` (e.g., `wordpress.rds`)
- Public package design for external embedding
- AWS SDK client initialization with retry and logging configuration

**Affected Components:**

- `internal/controller/rds/` → `rds/` (package relocation)
- `componentkit/controller` (dependency on namespacing helper - already exists)
- `cmd/main.go` (registration pattern update)

**Key Integration Points:**

- Uses `componentkit.ComponentReconciler` base controller
- Uses existing `componentkit.DefaultComponentReconcilerConfig(provider)` - no changes needed
- Creates AWS RDS client with retry configuration during initialization
- No external Kubernetes dependencies (RDS provider is AWS-only)

**Major Constraints:**

- Must maintain backward compatibility for standalone deployments
- AWS client initialization must handle credential resolution
- Cannot break existing RDS controller functionality
- Componentkit is provider-name agnostic - no changes required

## API Changes

**New Function:**

- `Register(mgr ctrl.Manager, namespace string) error`: Creates and registers RDS controller with namespaced provider name. Simplifies AWS client initialization.

**Modified Function:**

- `NewComponentReconciler(providerName string) (*ComponentReconciler, error)`: Add providerName parameter as first argument. Accepts any provider name including namespaced (e.g., "wordpress.rds").

**Package Structure Change:**

```text
OLD: internal/controller/rds/
NEW: rds/
     ├── controller.go       (public ComponentReconciler)
     ├── operations.go       (AWS RDS SDK integration)
     ├── operations_apply.go (RDS instance creation/update)
     ├── operations_delete.go (RDS instance deletion)
     ├── config.go           (RDS configuration parsing)
     ├── config_test.go      (configuration tests)
     ├── conversion.go       (RDS status conversion)
     └── register.go         (NEW - Register function)
```

**No Type Changes:**

- `ComponentReconciler` structure remains unchanged (embeds `componentkit.ComponentReconciler`)
- `RdsConfig` structure remains unchanged
- Operations factory types remain unchanged

## Critical Logic and Decisions

### Component: Register Function

**Key responsibilities:**

- Accept manager and namespace parameters
- Create ComponentReconciler with namespaced provider name
- Register with manager
- Return any initialization errors

**Critical flow:**

```text
when Register(mgr, namespace) is called:
  determine providerName based on namespace:
    if namespace is empty:
      providerName = "rds"
    else:
      providerName = namespace + "-rds"
  
  controller, err = NewComponentReconciler(providerName)
  if err != nil:
    return err
  
  return controller.SetupWithManager(mgr)
```

**Design decisions:**

- Decision: Namespace parameter for consistency - Rationale: Matches helm/manifest/configreader pattern
- Decision: Keep NewComponentReconciler public - Rationale: Allow advanced users direct access
- Decision: Simple provider name determination - Rationale: RDS provider doesn't need manager reference

### Component: NewComponentReconciler

**Key responsibilities:**

- Accept providerName parameter (first arg)
- Create AWS RDS operations factory
- Create provider config with custom requeue intervals
- Return configured ComponentReconciler

**Critical flow:**

```text
when NewComponentReconciler(providerName) is called:
  create rdsOperationsFactory (AWS client created internally)
  
  config = DefaultComponentReconcilerConfig(providerName)
  # This creates finalizer: {providerName}.componator.io/lifecycle
  # Example: "wordpress.rds.componator.io/lifecycle"
  
  config.ErrorRequeue = 15s      # RDS-specific timing
  config.DefaultRequeue = 30s
  config.StatusCheckRequeue = 30s
  
  return ComponentReconciler with componentkit base and factory
```

**Design decisions:**

- Decision: providerName as first parameter - Rationale: Enables namespacing, consistent with k8s providers
- Decision: Use existing componentkit API - Rationale: No componentkit changes needed
- Decision: Maintain RDS-specific requeue intervals - Rationale: RDS operations have different timing characteristics
- Decision: No manager parameter needed - Rationale: RDS provider only needs AWS credentials, not K8s client

### Component: AWS Client Management

**Unchanged logic:**

- AWS SDK client creation in operations factory
- Retry configuration (MaxAttempts: 10)
- AWS credential resolution from environment/IAM
- Error classification for retryable AWS errors

**Design decisions:**

- Decision: Keep AWS client creation in operations factory - Rationale: Works correctly, no K8s dependencies
- Decision: Maintain retry configuration - Rationale: AWS throttling requires aggressive retry

## Testing Approach

**Unit Tests:**

- Register function error handling
- NewComponentReconciler with providerName parameter
- Backward compatibility (simple "rds" produces "rds.componator.io/lifecycle")
- Namespaced provider produces correct finalizer ("wordpress.rds.componator.io/lifecycle")
- Configuration parsing with various RDS configs
- Status conversion from AWS RDS instance states

**Integration Tests:**

- RDS controller registration with namespace via Register()
- Component claiming with namespaced provider (`wordpress.rds`)
- RDS instance creation with namespaced controller
- Finalizer patterns with namespaced names
- AWS credential resolution in embedded context

**Critical Scenarios:**

- `test_register_with_namespace` - Verify wordpress.rds controller created
- `test_register_without_namespace` - Verify rds controller created (backward compat)
- `test_rds_deployment_namespaced` - End-to-end RDS instance creation with namespaced provider
- `test_multiple_namespaced_controllers` - Verify wordpress.rds and drupal.rds coexist

## Implementation Phases

### Phase 1: Relocate RDS Package

- Move `internal/controller/rds/` to `rds/`
- Update all internal imports in rds package
- Update cmd/main.go import path
- Deliverable: RDS controller builds and runs from new location

### Phase 2: Add Namespace Support

- Update `NewComponentReconciler()` signature with providerName parameter
- Implement provider name in config creation
- Update cmd/main.go to pass "rds" as providerName
- Deliverable: RDS controller works with providerName parameter, backward compatible

### Phase 3: Add Register Function

- Create `rds/register.go` with Register() function
- Implement namespace-based provider name determination
- Update cmd/main.go to use Register() instead of manual setup
- Deliverable: Register() function works for standalone deployment

### Phase 4: Integration Testing

- Create integration tests for namespaced scenarios
- Test AWS credential resolution with namespaced controller
- Validate finalizer and event naming patterns
- Deliverable: All integration tests pass, namespaced RDS provider validated

## Open Questions

**Question:** Should all four AWS providers (rds, iam-role, iam-policy, secret-push) be refactored together or separately?

- **Answer:** Coordinated implementation recommended - they follow identical patterns and share AWS SDK dependency management.

**Question:** Do AWS providers need any manager dependencies?

- **Answer:** No - AWS providers only need AWS credentials from environment/IAM. Unlike k8s providers (helm needs k8s client for OCI, manifest needs dynamic client), AWS providers are purely AWS SDK-based.
