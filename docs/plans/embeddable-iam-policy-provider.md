# Implementation Plan: Embeddable IAM Policy Provider

## Feature Overview

Make the IAM Policy component provider embeddable in setkit distributions by moving it from `internal/controller/iam-policy/` to a public `iampolicy/` package with namespace support and a `Register()` function. This enables setkits to embed the IAM Policy provider for creating AWS IAM policies while preventing conflicts when multiple setkits coexist.

## Architecture Impact

**Architectural Patterns:**

- Component provider claiming protocol (handler-specific finalizers)
- Provider namespacing pattern: `{setkit}.{provider}` (e.g., `wordpress.iam-policy`)
- Public package design for external embedding
- AWS SDK client initialization with retry and logging configuration

**Affected Components:**

- `internal/controller/iam-policy/` → `iampolicy/` (package relocation)
- `componentkit/controller` (dependency on namespacing helper - already exists)
- `cmd/main.go` (registration pattern update)

**Key Integration Points:**

- Uses `componentkit.ComponentReconciler` base controller
- Uses existing `componentkit.DefaultComponentReconcilerConfig(provider)` - no changes needed
- Creates AWS IAM client with retry configuration during initialization
- No external Kubernetes dependencies (IAM provider is AWS-only)

**Major Constraints:**

- Must maintain backward compatibility for standalone deployments
- AWS client initialization must handle credential resolution
- Cannot break existing IAM Policy controller functionality
- Componentkit is provider-name agnostic - no changes required

## API Changes

**New Function:**

- `Register(mgr ctrl.Manager, namespace string) error`: Creates and registers IAM Policy controller with namespaced provider name.

**Modified Function:**

- `NewComponentReconciler(providerName string) (*ComponentReconciler, error)`: Add providerName parameter as first argument. Accepts any provider name including namespaced (e.g., "wordpress.iam-policy").

**Package Structure Change:**

```text
OLD: internal/controller/iam-policy/
NEW: iampolicy/
     ├── controller.go       (public ComponentReconciler)
     ├── operations.go       (AWS IAM SDK integration)
     ├── operations_apply.go (IAM policy creation/update)
     ├── operations_delete.go (IAM policy deletion)
     ├── config.go           (IAM policy configuration parsing)
     ├── config_test.go      (configuration tests)
     └── register.go         (NEW - Register function)
```

**No Type Changes:**

- `ComponentReconciler` structure remains unchanged (embeds `componentkit.ComponentReconciler`)
- `IamPolicyConfig` structure remains unchanged
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
      providerName = "iam-policy"
    else:
      providerName = namespace + "-iam-policy"
  
  controller, err = NewComponentReconciler(providerName)
  if err != nil:
    return err
  
  return controller.SetupWithManager(mgr)
```

**Design decisions:**

- Decision: Namespace parameter for consistency - Rationale: Matches all other provider patterns
- Decision: Keep NewComponentReconciler public - Rationale: Allow advanced users direct access
- Decision: Simple provider name determination - Rationale: IAM provider doesn't need manager reference

### Component: NewComponentReconciler

**Key responsibilities:**

- Accept providerName parameter (first arg)
- Create AWS IAM operations factory
- Create provider config with custom requeue intervals
- Return configured ComponentReconciler

**Critical flow:**

```text
when NewComponentReconciler(providerName) is called:
  create iamPolicyOperationsFactory (AWS client created internally)
  
  config = DefaultComponentReconcilerConfig(providerName)
  # This creates finalizer: {providerName}.componator.io/lifecycle
  # Example: "wordpress.iam-policy.componator.io/lifecycle"
  
  config.ErrorRequeue = 30s      # IAM-specific timing (AWS throttling tolerance)
  config.DefaultRequeue = 15s    # IAM operations are fast
  config.StatusCheckRequeue = 10s
  
  return ComponentReconciler with componentkit base and factory
```

**Design decisions:**

- Decision: providerName as first parameter - Rationale: Enables namespacing, consistent with other providers
- Decision: Use existing componentkit API - Rationale: No componentkit changes needed
- Decision: Maintain IAM-specific requeue intervals - Rationale: Same timing characteristics as IAM Role
- Decision: No manager parameter needed - Rationale: IAM provider only needs AWS credentials

### Component: AWS Client Management

**Unchanged logic:**

- AWS SDK client creation in operations factory
- Retry configuration (MaxAttempts: 10)
- AWS credential resolution from environment/IAM
- Error classification for retryable AWS errors (throttling, service unavailable)

**Design decisions:**

- Decision: Keep AWS client creation in operations factory - Rationale: Works correctly, no K8s dependencies
- Decision: Maintain retry configuration - Rationale: IAM has strict rate limits requiring aggressive retry

## Testing Approach

**Unit Tests:**

- Register function error handling
- NewComponentReconciler with providerName parameter
- Backward compatibility (simple "iam-policy" produces "iam-policy.componator.io/lifecycle")
- Namespaced provider produces correct finalizer ("wordpress.iam-policy.componator.io/lifecycle")
- Configuration parsing with various IAM policy configs
- Policy document validation

**Integration Tests:**

- IAM Policy controller registration with namespace via Register()
- Component claiming with namespaced provider (`wordpress.iam-policy`)
- IAM policy creation with namespaced controller
- Policy document updates with namespaced controller
- Finalizer patterns with namespaced names

**Critical Scenarios:**

- `test_register_with_namespace` - Verify wordpress.iam-policy controller created
- `test_register_without_namespace` - Verify iam-policy controller created (backward compat)
- `test_iam_policy_deployment_namespaced` - End-to-end IAM policy creation with namespaced provider
- `test_multiple_namespaced_controllers` - Verify wordpress.iam-policy and drupal.iam-policy coexist

## Implementation Phases

### Phase 1: Relocate IAM Policy Package

- Move `internal/controller/iam-policy/` to `iampolicy/`
- Update all internal imports in iampolicy package
- Update cmd/main.go import path
- Deliverable: IAM Policy controller builds and runs from new location

### Phase 2: Add Namespace Support

- Update `NewComponentReconciler()` signature with providerName parameter
- Implement provider name in config creation
- Update cmd/main.go to pass "iam-policy" as providerName
- Deliverable: IAM Policy controller works with providerName parameter, backward compatible

### Phase 3: Add Register Function

- Create `iampolicy/register.go` with Register() function
- Implement namespace-based provider name determination
- Update cmd/main.go to use Register() instead of manual setup
- Deliverable: Register() function works for standalone deployment

### Phase 4: Integration Testing

- Create integration tests for namespaced scenarios
- Test AWS credential resolution with namespaced controller
- Validate finalizer and event naming patterns
- Deliverable: All integration tests pass, namespaced IAM Policy provider validated

## Open Questions

**Question:** Should package name be `iampolicy` or `iam-policy`?

- **Answer:** Use `iampolicy` (Go package naming convention avoids hyphens). The provider name remains "iam-policy" for backward compatibility.

**Question:** Should all four AWS providers be refactored together?

- **Answer:** Yes - coordinated implementation recommended for consistency.
