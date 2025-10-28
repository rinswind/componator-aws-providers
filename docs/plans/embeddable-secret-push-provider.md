# Implementation Plan: Embeddable Secret Push Provider

## Feature Overview

Make the Secret Push component provider embeddable in setkit distributions by moving it from `internal/controller/secret-push/` to a public `secretpush/` package with namespace support and a `Register()` function. This enables setkits to embed the Secret Push provider for pushing Kubernetes secrets to AWS Secrets Manager while preventing conflicts when multiple setkits coexist.

## Architecture Impact

**Architectural Patterns:**

- Component provider claiming protocol (handler-specific finalizers)
- Provider namespacing pattern: `{setkit}.{provider}` (e.g., `wordpress.secret-push`)
- Public package design for external embedding
- AWS SDK client initialization with retry and logging configuration
- **Unique requirement**: Kubernetes Secret watch (unlike other AWS providers)

**Affected Components:**

- `internal/controller/secret-push/` → `secretpush/` (package relocation)
- `componentkit/controller` (dependency on namespacing helper - already exists)
- `cmd/main.go` (registration pattern update)

**Key Integration Points:**

- Uses `componentkit.ComponentReconciler` base controller
- Uses existing `componentkit.DefaultComponentReconcilerConfig(provider)` - no changes needed
- Creates AWS Secrets Manager client with retry configuration during initialization
- **Watches Kubernetes Secrets** - requires k8s client from manager (unlike other AWS providers)

**Major Constraints:**

- Must maintain backward compatibility for standalone deployments
- AWS client initialization must handle credential resolution
- **K8s client required** - Secret Push watches Kubernetes Secrets and needs client to read them
- Cannot break existing Secret Push controller functionality
- Componentkit is provider-name agnostic - no changes required

## API Changes

**New Function:**

- `Register(mgr ctrl.Manager, namespace string) error`: Creates and registers Secret Push controller with namespaced provider name.

**Modified Function:**

- `NewComponentReconciler(providerName string, k8sClient client.Client) (*ComponentReconciler, error)`: Add providerName parameter as first argument AND k8sClient parameter. Accepts any provider name including namespaced (e.g., "wordpress.secret-push").

**Package Structure Change:**

```text
OLD: internal/controller/secret-push/
NEW: secretpush/
     ├── controller.go       (public ComponentReconciler)
     ├── operations.go       (AWS Secrets Manager SDK integration)
     ├── operations_apply.go (Secret push to AWS)
     ├── operations_delete.go (Secret deletion from AWS)
     ├── config.go           (Secret push configuration parsing)
     ├── config_test.go      (configuration tests)
     └── register.go         (NEW - Register function)
```

**Type Changes:**

- `ComponentReconciler` needs to store k8s client for Secret watch mapping
- `SecretPushOperationsFactory` may need k8s client reference for reading Secrets during reconciliation

## Critical Logic and Decisions

### Component: Register Function

**Key responsibilities:**

- Accept manager and namespace parameters
- Extract k8s client from manager
- Create ComponentReconciler with namespaced provider name and client
- Register with manager
- Return any initialization errors

**Critical flow:**

```text
when Register(mgr, namespace) is called:
  determine providerName based on namespace:
    if namespace is empty:
      providerName = "secret-push"
    else:
      providerName = namespace + "-secret-push"
  
  client = mgr.GetClient()
  
  controller, err = NewComponentReconciler(providerName, client)
  if err != nil:
    return err
  
  return controller.SetupWithManager(mgr)
```

**Design decisions:**

- Decision: Extract client in Register() - Rationale: Hide complexity, similar to helm provider pattern
- Decision: Namespace parameter for consistency - Rationale: Matches all other provider patterns
- Decision: Keep NewComponentReconciler public - Rationale: Allow advanced users direct access

### Component: NewComponentReconciler

**Key responsibilities:**

- Accept providerName parameter (first arg)
- Accept k8sClient parameter (for Secret reading)
- Create AWS Secrets Manager operations factory
- Create provider config with custom requeue intervals
- Store client for potential Secret watch mapping
- Return configured ComponentReconciler

**Critical flow:**

```text
when NewComponentReconciler(providerName, k8sClient) is called:
  create secretPushOperationsFactory with k8sClient
  # Factory needs client to read K8s Secrets during reconciliation
  
  config = DefaultComponentReconcilerConfig(providerName)
  # This creates finalizer: {providerName}.componator.io/lifecycle
  # Example: "wordpress.secret-push.componator.io/lifecycle"
  
  config.ErrorRequeue = 30s      # AWS-specific timing (throttling tolerance)
  config.DefaultRequeue = 30s    # Secrets Manager operations
  config.StatusCheckRequeue = 15s
  
  return ComponentReconciler with:
    - componentkit base
    - factory
    - k8sClient (stored for potential Secret watch)
```

**Design decisions:**

- Decision: providerName as first parameter - Rationale: Enables namespacing, consistent with other providers
- Decision: k8sClient as second parameter - Rationale: Required for reading Kubernetes Secrets, matches helm pattern
- Decision: Use existing componentkit API - Rationale: No componentkit changes needed
- Decision: Store client in ComponentReconciler - Rationale: May be needed for Secret watch mapping
- Decision: Pass client to operations factory - Rationale: Factory needs to read Secrets during reconciliation

### Component: Kubernetes Secret Integration

**Key consideration:**

- Secret Push reads Kubernetes Secrets referenced in Component.Spec.Config
- Operations factory needs k8s client to read Secrets
- May need Secret watch with mapping to Components (similar to configreader pattern)
- Unlike pure AWS providers, this has hybrid K8s + AWS dependencies

**Critical flow for Secret reading:**

```text
when Apply operation is called:
  parse Component.Spec.Config to get secretRef
  use k8sClient to read Secret from secretRef namespace/name
  extract secret data from Secret
  push secret data to AWS Secrets Manager
```

**Design decisions:**

- Decision: Pass k8s client to operations factory - Rationale: Factory needs direct Secret access
- Decision: Consider Secret watch in future - Rationale: Not in initial implementation, but architecture should support it
- Decision: Validate Secret exists during reconciliation - Rationale: Clear error messages for missing Secrets

### Component: AWS Client Management

**Unchanged logic:**

- AWS SDK client creation in operations factory
- Retry configuration (MaxAttempts: 10)
- AWS credential resolution from environment/IAM
- Error classification for retryable AWS errors

**Design decisions:**

- Decision: Keep AWS client creation in operations factory - Rationale: Works correctly
- Decision: Maintain retry configuration - Rationale: AWS throttling requires aggressive retry

## Testing Approach

**Unit Tests:**

- Register function with client extraction
- NewComponentReconciler with providerName and k8sClient parameters
- Backward compatibility (simple "secret-push" produces "secret-push.componator.io/lifecycle")
- Namespaced provider produces correct finalizer ("wordpress.secret-push.componator.io/lifecycle")
- Configuration parsing with Secret references
- Secret reading with valid and missing Secrets

**Integration Tests:**

- Secret Push controller registration with namespace via Register()
- Component claiming with namespaced provider (`wordpress.secret-push`)
- Secret push to AWS with namespaced controller
- Kubernetes Secret reading during reconciliation
- Finalizer patterns with namespaced names

**Critical Scenarios:**

- `test_register_with_namespace` - Verify wordpress.secret-push controller created
- `test_register_without_namespace` - Verify secret-push controller created (backward compat)
- `test_secret_push_namespaced` - End-to-end Secret push to AWS with namespaced provider
- `test_kubernetes_secret_reading` - Verify k8s Secret is read correctly during reconciliation
- `test_multiple_namespaced_controllers` - Verify wordpress.secret-push and drupal.secret-push coexist

## Implementation Phases

### Phase 1: Relocate Secret Push Package

- Move `internal/controller/secret-push/` to `secretpush/`
- Update all internal imports in secretpush package
- Update cmd/main.go import path
- Deliverable: Secret Push controller builds and runs from new location

### Phase 2: Add Namespace and Client Support

- Update `NewComponentReconciler()` signature with providerName and k8sClient parameters
- Update operations factory to accept k8sClient
- Implement provider name in config creation
- Update cmd/main.go to pass "secret-push" as providerName and extract client
- Deliverable: Secret Push controller works with providerName and client parameters, backward compatible

### Phase 3: Add Register Function

- Create `secretpush/register.go` with Register() function
- Implement namespace-based provider name determination
- Implement client extraction from manager
- Update cmd/main.go to use Register() instead of manual setup
- Deliverable: Register() function works for standalone deployment

### Phase 4: Validate K8s Secret Integration

- Test Secret reading with k8s client
- Verify error handling for missing Secrets
- Test Secret data extraction and AWS push
- Deliverable: Secret reading works correctly with namespaced provider

### Phase 5: Integration Testing

- Create integration tests for namespaced scenarios
- Test AWS + K8s integration with namespaced controller
- Validate finalizer and event naming patterns
- Deliverable: All integration tests pass, namespaced Secret Push provider validated

## Open Questions

**Question:** Should package name be `secretpush` or `secret-push`?

- **Answer:** Use `secretpush` (Go package naming convention avoids hyphens). The provider name remains "secret-push" for backward compatibility.

**Question:** Should Secret watch be implemented now or later?

- **Answer:** Later. Initial implementation focuses on on-demand Secret reading during reconciliation. Secret watch can be added as enhancement if needed.

**Question:** Should all four AWS providers be refactored together?

- **Answer:** Yes - coordinated implementation recommended. Secret Push is unique because it needs k8s client, but shares patterns with other AWS providers.

**Question:** Does operations factory need k8s client?

- **Answer:** Yes - Secret Push operations need to read Kubernetes Secrets during Apply operation. This is passed to factory constructor.
