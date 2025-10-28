# AWS Providers Embeddability - Master Implementation Plan

## Overview

Make all AWS component providers embeddable in setkit distributions by relocating them from `internal/controller/` to public packages with namespace support and `Register()` functions. This enables setkits to embed AWS providers directly while preventing conflicts when multiple setkits coexist.

## Providers

All four AWS providers will be refactored following the same pattern established by componator-k8s-providers:

1. **RDS Provider** - AWS RDS database instance management
2. **IAM Role Provider** - AWS IAM role creation and trust policies
3. **IAM Policy Provider** - AWS IAM policy document management
4. **Secret Push Provider** - Kubernetes Secret to AWS Secrets Manager synchronization

## Common Pattern

All providers follow this transformation:

```text
OLD: internal/controller/{provider}/
NEW: {provider}/
     ├── controller.go        (public ComponentReconciler)
     ├── operations.go        (AWS SDK integration)
     ├── operations_apply.go  (resource creation/update)
     ├── operations_delete.go (resource deletion)
     ├── config.go            (configuration parsing)
     ├── config_test.go       (configuration tests)
     └── register.go          (NEW - Register function)
```

### Package Names

Following Go conventions (no hyphens in package names):

- `internal/controller/rds/` → `rds/` (provider name: "rds")
- `internal/controller/iam-role/` → `iamrole/` (provider name: "iam-role")
- `internal/controller/iam-policy/` → `iampolicy/` (provider name: "iam-policy")
- `internal/controller/secret-push/` → `secretpush/` (provider name: "secret-push")

## API Pattern

### Register Function (all providers)

```go
// Register creates and registers a {Provider} controller with namespaced provider name.
//
// Parameters:
//   - mgr: The controller-runtime manager
//   - namespace: Setkit namespace (e.g., "wordpress"), or "" for standalone
//
// Returns:
//   - error: Initialization or registration errors
func Register(mgr ctrl.Manager, namespace string) error
```

### NewComponentReconciler Signature

**Pure AWS providers (rds, iamrole, iampolicy):**

```go
func NewComponentReconciler(providerName string) (*ComponentReconciler, error)
```

**Hybrid K8s+AWS provider (secretpush):**

```go
func NewComponentReconciler(providerName string, k8sClient client.Client) (*ComponentReconciler, error)
```

## Provider-Specific Considerations

### RDS Provider

- **Dependencies**: AWS SDK only
- **Requeue timings**: ErrorRequeue=15s, DefaultRequeue=30s, StatusCheckRequeue=30s
- **AWS operations**: RDS instance lifecycle (create, modify, delete, status checks)
- **No K8s dependencies**

### IAM Role Provider

- **Dependencies**: AWS SDK only
- **Requeue timings**: ErrorRequeue=30s, DefaultRequeue=15s, StatusCheckRequeue=10s
- **AWS operations**: IAM role lifecycle, trust policy management
- **No K8s dependencies**

### IAM Policy Provider

- **Dependencies**: AWS SDK only
- **Requeue timings**: ErrorRequeue=30s, DefaultRequeue=15s, StatusCheckRequeue=10s
- **AWS operations**: IAM policy lifecycle, policy document management
- **No K8s dependencies**

### Secret Push Provider (Unique)

- **Dependencies**: AWS SDK + Kubernetes client
- **Requeue timings**: ErrorRequeue=30s, DefaultRequeue=30s, StatusCheckRequeue=15s
- **Hybrid operations**:
  - Reads Kubernetes Secrets (requires k8s client)
  - Pushes to AWS Secrets Manager (requires AWS SDK)
- **K8s client required**: Passed to both Register() and NewComponentReconciler()
- **Operations factory needs client**: For reading Secrets during reconciliation

## Implementation Strategy

### Coordinated Implementation

All four providers should be refactored together because:

1. **Identical patterns**: All follow the same Register() + NewComponentReconciler() pattern
2. **Shared testing**: Can validate multi-provider scenarios together
3. **Consistent cmd/main.go updates**: Single update to registration logic
4. **Cross-provider validation**: Ensure namespacing works across all AWS providers

### Implementation Order

**Recommended phases across all providers:**

1. **Phase 1: Package Relocation**
   - Move all four providers from internal/ to public packages
   - Update imports in cmd/main.go
   - Verify builds and existing tests pass
   - Deliverable: All providers build from new locations

2. **Phase 2: Add Namespace Support**
   - Update NewComponentReconciler() signatures for all providers
   - Add providerName parameter (and k8sClient for secretpush)
   - Update cmd/main.go to pass provider names
   - Deliverable: All providers accept providerName parameter

3. **Phase 3: Add Register Functions**
   - Create register.go for all four providers
   - Update cmd/main.go to use Register() pattern
   - Deliverable: All providers use Register() API

4. **Phase 4: Integration Testing**
   - Test namespaced scenarios for all providers
   - Test multi-setkit coexistence
   - Validate finalizer and event naming
   - Deliverable: All providers validated with namespacing

## Usage Examples

### Standalone Deployment (componator-aws-providers/cmd/main.go)

```go
import (
    "github.com/rinswind/componator-aws-providers/rds"
    "github.com/rinswind/componator-aws-providers/iamrole"
    "github.com/rinswind/componator-aws-providers/iampolicy"
    "github.com/rinswind/componator-aws-providers/secretpush"
)

func main() {
    mgr, err := ctrl.NewManager(...)
    
    // Register all AWS providers without namespace (standalone mode)
    if err := rds.Register(mgr, ""); err != nil {
        setupLog.Error(err, "unable to register rds controller")
        os.Exit(1)
    }
    
    if err := iamrole.Register(mgr, ""); err != nil {
        setupLog.Error(err, "unable to register iam-role controller")
        os.Exit(1)
    }
    
    if err := iampolicy.Register(mgr, ""); err != nil {
        setupLog.Error(err, "unable to register iam-policy controller")
        os.Exit(1)
    }
    
    if err := secretpush.Register(mgr, ""); err != nil {
        setupLog.Error(err, "unable to register secret-push controller")
        os.Exit(1)
    }
    
    mgr.Start(ctrl.SetupSignalHandler())
}
```

### Setkit Embedding (wordpress-operator/cmd/main.go)

```go
import (
    "github.com/rinswind/componator-aws-providers/rds"
    "github.com/rinswind/componator-aws-providers/iamrole"
    "github.com/rinswind/componator-aws-providers/secretpush"
)

func main() {
    mgr, err := ctrl.NewManager(...)
    
    // Register AWS providers with "wordpress" namespace
    if err := rds.Register(mgr, "wordpress"); err != nil {
        setupLog.Error(err, "unable to register wordpress.rds provider")
        os.Exit(1)
    }
    
    if err := iamrole.Register(mgr, "wordpress"); err != nil {
        setupLog.Error(err, "unable to register wordpress.iam-role provider")
        os.Exit(1)
    }
    
    if err := secretpush.Register(mgr, "wordpress"); err != nil {
        setupLog.Error(err, "unable to register wordpress.secret-push provider")
        os.Exit(1)
    }
    
    // wordpress.rds will claim Components with spec.provider = "wordpress-rds"
    // wordpress.iam-role will claim Components with spec.provider = "wordpress-iam-role"
    // wordpress.secret-push will claim Components with spec.provider = "wordpress-secret-push"
    
    mgr.Start(ctrl.SetupSignalHandler())
}
```

## Provider Name Format

When namespace is provided, provider names follow `{namespace}-{provider}` pattern:

| Namespace | Provider Base | Resulting Provider Name | Finalizer |
|-----------|---------------|------------------------|-----------|
| "" | rds | rds | rds.componator.io/lifecycle |
| "wordpress" | rds | wordpress-rds | wordpress-rds.componator.io/lifecycle |
| "" | iam-role | iam-role | iam-role.componator.io/lifecycle |
| "wordpress" | iam-role | wordpress-iam-role | wordpress-iam-role.componator.io/lifecycle |
| "" | secret-push | secret-push | secret-push.componator.io/lifecycle |
| "wordpress" | secret-push | wordpress-secret-push | wordpress-secret-push.componator.io/lifecycle |

## Testing Strategy

### Unit Tests (per provider)

- Register() function with valid/invalid parameters
- NewComponentReconciler() with providerName variations
- Backward compatibility (empty namespace)
- Configuration parsing with provider-specific configs

### Integration Tests (cross-provider)

- Multiple namespaced controllers in same cluster
- Provider isolation (wordpress-rds vs drupal-rds)
- Finalizer and event naming validation
- Component claiming with correct provider names

### Critical Scenarios

1. **Standalone mode**: All providers work with empty namespace
2. **Setkit mode**: All providers work with namespace prefix
3. **Multi-setkit**: Multiple setkits with same providers coexist
4. **Provider isolation**: Each namespaced provider only claims its Components

## Architecture Compliance

All implementations must follow:

- **Claiming Protocol**: Handler-specific finalizers with namespaced names
- **Creation Protocol**: Immediate resource creation and status reporting
- **Deletion Protocol**: Finalizer-based cleanup coordination
- **Componentkit Integration**: Use `DefaultComponentReconcilerConfig(providerName)`
- **No componentkit changes**: Componentkit is provider-name agnostic

## Dependencies

### Go Module Dependencies

All providers depend on:

- `github.com/rinswind/componator` - CRD definitions and componentkit
- `github.com/aws/aws-sdk-go-v2` - AWS SDK
- `sigs.k8s.io/controller-runtime` - Controller framework

Secret Push additionally depends on:

- Kubernetes client for reading Secrets

### Development Dependencies

- Local replace directive for componator during development
- AWS credentials for testing (via environment or IAM roles)
- Kubernetes cluster for integration tests

## Open Questions

**Q: Should we validate that all AWS providers can be embedded in the same setkit?**

A: Yes - wordpress-operator should be able to embed all four AWS providers simultaneously. Test in Phase 4.

**Q: Do we need field indexers for any AWS providers?**

A: Not in initial implementation. ConfigReader needs field indexer because it watches ConfigMaps. AWS providers could add Secret watch to secret-push later, which would need field indexer.

**Q: Should provider base names use hyphens or not?**

A: Use hyphens in provider names for backward compatibility ("iam-role", "iam-policy", "secret-push"). Package names avoid hyphens per Go convention (iamrole, iampolicy, secretpush).

## Related Documentation

- Individual provider plans:
  - `embeddable-rds-provider.md`
  - `embeddable-iam-role-provider.md`
  - `embeddable-iam-policy-provider.md`
  - `embeddable-secret-push-provider.md`
- K8s providers reference implementation:
  - `../componator-k8s-providers/docs/plans/embeddable-helm-provider.md`
  - `../componator-k8s-providers/docs/plans/embeddable-manifest-provider.md`
  - `../componator-k8s-providers/docs/plans/embeddable-configreader-provider.md`
