# Component Handler Implementation Guide

This directory contains Component handler controllers that implement specific deployment technologies (Terraform, Helm, etc.) following the deployment orchestration protocols.

## Architecture Overview

The controller architecture separates generic protocol logic from handler-specific operations:

- **Generic Base Controller**: Available from `github.com/rinswind/deployment-operator/handler/base` - handles all protocol state machine logic, finalizer management, and status transitions
- **ComponentOperations Interface**: Defines the contract for handler-specific deployment operations  
- **Handler-Specific Implementations**: Each handler (Helm, RDS, etc.) implements the operations interface and uses the generic base

This architecture achieves **code reuse**, **protocol compliance**, and **extensibility** while maintaining **backward compatibility**.

**Important**: The base controller is now provided by the deployment-operator project as part of the complete handler toolkit.

## Quick Start: Implementing a New Handler

### 1. Create Handler Directory Structure

```txt
internal/controller/{handler-name}/
├── controller.go       # Main controller with generic base composition
├── operations.go       # ComponentOperations interface implementation  
├── operations_*.go     # Handler-specific operation implementations
├── controller_test.go  # Unit tests
└── README.md          # Handler-specific documentation
```

### 2. Implement ComponentOperations Interface

```go
// internal/controller/{handler-name}/operations.go
package handlername

import (
    "context"
    "time"
    "github.com/rinswind/deployment-operator/handler/base"
    v1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
)

const (
    HandlerName    = "handler-name"
    ControllerName = "handler-name-component"
)

// HandlerOperations implements ComponentOperations for handler-specific deployments
type HandlerOperations struct {
    // Add handler-specific fields (clients, config, etc.)
}

// NewHandlerOperations creates a new operations instance
func NewHandlerOperations() *HandlerOperations {
    return &HandlerOperations{}
}

// NewHandlerOperationsConfig creates configuration with handler-specific settings
func NewHandlerOperationsConfig() base.ComponentHandlerConfig {
    config := base.DefaultComponentHandlerConfig(HandlerName, ControllerName)
    
    // Customize timeouts and requeue periods as needed
    config.DefaultRequeue = 15 * time.Second
    config.StatusCheckRequeue = 10 * time.Second
    config.ErrorRequeue = 30 * time.Second
    
    return config
}

// Deploy implements the deployment operation
func (op *HandlerOperations) Deploy(ctx context.Context, component *v1alpha1.Component) (bool, error, error) {
    // Implement deployment logic
    // Return (success, ioError, businessError)
    return false, nil, errors.New("not implemented")
}

// CheckDeploymentReady checks if deployment is complete and ready
func (op *HandlerOperations) CheckDeploymentReady(ctx context.Context, component *v1alpha1.Component) (bool, error, error) {
    // Check readiness logic
    return false, nil, errors.New("not implemented")  
}

// Upgrade implements upgrade operations
func (op *HandlerOperations) Upgrade(ctx context.Context, component *v1alpha1.Component) (bool, error, error) {
    // Upgrade logic
    return false, nil, errors.New("not implemented")
}

// Delete implements deletion operations  
func (op *HandlerOperations) Delete(ctx context.Context, component *v1alpha1.Component) (bool, error, error) {
    // Deletion logic
    return false, nil, errors.New("not implemented")
}

// CheckDeletionComplete verifies deletion is complete
func (op *HandlerOperations) CheckDeletionComplete(ctx context.Context, component *v1alpha1.Component) (bool, error, error) {
    // Check deletion status
    return false, nil, errors.New("not implemented")
}
```

### 3. Create Controller with Generic Base

```go
// internal/controller/{handler-name}/controller.go  
package handlername

import (
    "github.com/rinswind/deployment-operator/handler/base"
)

//+kubebuilder:rbac:groups=deployments.deployment-orchestrator.io,resources=components,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=deployments.deployment-orchestrator.io,resources=components/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=deployments.deployment-orchestrator.io,resources=components/finalizers,verbs=update

// ComponentReconciler reconciles Components using the generic controller base
type ComponentReconciler struct {
    *base.ComponentReconciler
}

// NewComponentReconciler creates a controller with handler operations
func NewComponentReconciler() *ComponentReconciler {
    operations := NewHandlerOperations()
    config := NewHandlerOperationsConfig()
    
    return &ComponentReconciler{
        ComponentReconciler: base.NewComponentReconciler(operations, config),
    }
}
```

**Important - RBAC Annotations**: Each handler controller **must** include the kubebuilder RBAC annotations shown above. These annotations generate the necessary RBAC permissions for Component resource access. The generic base controller no longer includes these annotations - they must be added to each individual handler.

### 4. Add Unit Tests

```go
// internal/controller/{handler-name}/controller_test.go
func TestHandlerController(t *testing.T) {
    // Set up test environment with envtest
    
    component := &v1alpha1.Component{
        Spec: v1alpha1.ComponentSpec{
            Handler: HandlerName,
            // ... component spec
        },
    }
    
    // Create and configure reconciler
    reconciler := NewComponentReconciler()
    reconciler.Client = k8sClient
    reconciler.Scheme = scheme.Scheme
    
    // Test reconciliation
    result, err := reconciler.Reconcile(ctx, req)
    // ... assertions
}
```

## ComponentOperations Interface Reference

The `base.ComponentOperations` interface defines the contract all handlers must implement:

```go
type ComponentOperations interface {
    // Deploy performs the initial deployment
    Deploy(ctx context.Context, component *v1alpha1.Component) (success bool, ioError error, businessError error)
    
    // CheckDeploymentReady verifies deployment completion
    CheckDeploymentReady(ctx context.Context, component *v1alpha1.Component) (ready bool, ioError error, businessError error)
    
    // Upgrade handles component updates
    Upgrade(ctx context.Context, component *v1alpha1.Component) (success bool, ioError error, businessError error)
    
    // Delete performs cleanup operations
    Delete(ctx context.Context, component *v1alpha1.Component) (success bool, ioError error, businessError error)
    
    // CheckDeletionComplete verifies cleanup completion
    CheckDeletionComplete(ctx context.Context, component *v1alpha1.Component) (complete bool, ioError error, businessError error)
}
```

### Error Handling Pattern

Each operation method returns three values:

- **`success/ready/complete bool`**: Operation success status  
- **`ioError error`**: Transient I/O errors (network, API timeouts) that should trigger requeue
- **`businessError error`**: Business logic errors that should be reported but not retried

The generic base controller uses these return values to determine requeue behavior and status updates.

## Protocol Compliance (Handled by Generic Base)

The generic base controller automatically handles:

- **Resource Discovery**: Event filtering to only process matching handler Components
- **Claiming Protocol**: Atomic finalizer-based claiming with `util.ClaimingProtocolValidator`
- **Status Management**: Proper ComponentPhase transitions (Pending → Claimed → Deploying → Ready)
- **Error Handling**: Distinction between I/O errors (requeue) and business errors (fail)
- **Deletion Coordination**: Waiting for composition coordination before cleanup
- **Finalizer Management**: Adding/removing handler-specific finalizers

## Configuration Options

The `ComponentHandlerConfig` allows customization:

```go
type ComponentHandlerConfig struct {
    HandlerName        string        // Handler identifier
    ControllerName     string        // Controller name for manager registration
    DefaultRequeue     time.Duration // Default requeue interval
    StatusCheckRequeue time.Duration // Status check requeue interval  
    ErrorRequeue       time.Duration // Error condition requeue interval
}
```

Use `base.DefaultComponentHandlerConfig()` and customize as needed for your handler's timing requirements.

## Testing Patterns

### Testing Strategy with Generic Base

Since protocol logic is handled by the generic base controller, handler tests should focus on:

- **Configuration Parsing**: Validate handler-specific configuration validation
- **Operations Logic**: Test individual ComponentOperations interface methods with real implementations
- **Integration**: Test complete handler with generic base using envtest

**Note**: Placeholder implementations (like RDS stubs) don't need tests until real operations are implemented.

### Unit Tests with Mock Operations

Test the generic base controller with mock operations:

```go
type mockOperations struct{}
func (m *mockOperations) Deploy(ctx context.Context, component *v1alpha1.Component) (bool, error, error) {
    return true, nil, nil
}
// ... implement other interface methods

func TestControllerLogic(t *testing.T) {
    ops := &mockOperations{}
    config := base.DefaultComponentHandlerConfig("test", "test-controller")
    reconciler := base.NewComponentReconciler(ops, config)
    // Test with envtest...
}
```

### Integration Tests

Test actual handler implementations with the generic base:

```go
func TestHandlerIntegration(t *testing.T) {
    reconciler := NewComponentReconciler()
    reconciler.Client = k8sClient
    reconciler.Scheme = scheme.Scheme
    
    // Create test component
    // Call reconciler.Reconcile()  
    // Verify protocol compliance
}
```

## Current Handler Implementations

### Helm Handler (`internal/controller/helm/`)

- **Purpose**: Deploy and manage Helm charts
- **Operations**: Uses Helm SDK for chart installation, upgrades, and deletion
- **Configuration**: Configurable Helm repositories, namespaces, and values
- **Testing**: 13 passing tests focusing on configuration parsing and operations logic
- **Architecture**: Fully migrated to generic base controller with HelmOperations implementation

### RDS Handler (`internal/controller/rds/`)  

- **Purpose**: Deploy and manage AWS RDS instances
- **Status**: Architecture implemented with TODO placeholders for AWS SDK integration
- **Operations**: Placeholder implementations for RDS create, modify, delete operations
- **Testing**: Tests will be added when actual RDS operations are implemented
- **Next Steps**: Implement actual AWS RDS SDK integration and corresponding tests

## Advanced Patterns

### Handler-Specific Extensions

Handlers can extend the generic base through composition:

```go
type ComponentReconciler struct {
    *base.ComponentReconciler
    // Add handler-specific fields
    customClient SomeClient
}

func (r *ComponentReconciler) CustomMethod() error {
    // Handler-specific logic that uses the base controller
    return nil
}
```

### Multi-Version Support

Support multiple API versions by implementing version-specific operations:

```go
type MultiVersionOperations struct {
    v1Operations ComponentOperations
    v2Operations ComponentOperations  
}

func (mv *MultiVersionOperations) Deploy(ctx context.Context, component *v1alpha1.Component) (bool, error, error) {
    switch component.Spec.Version {
    case "v1":
        return mv.v1Operations.Deploy(ctx, component)
    case "v2":  
        return mv.v2Operations.Deploy(ctx, component)
    default:
        return false, nil, fmt.Errorf("unsupported version: %s", component.Spec.Version)
    }
}
```

### Custom Status Reporting

While the base controller handles standard phases, handlers can add custom status information:

```go
func (op *HandlerOperations) Deploy(ctx context.Context, component *v1alpha1.Component) (bool, error, error) {
    // Perform deployment...
    
    // Add custom status information
    if component.Status.Conditions == nil {
        component.Status.Conditions = []v1alpha1.ComponentCondition{}
    }
    
    component.Status.Conditions = append(component.Status.Conditions, v1alpha1.ComponentCondition{
        Type: "CustomCondition",
        Status: "True", 
        Message: "Handler-specific status information",
    })
    
    return true, nil, nil
}
```

## Migration from Legacy Controllers

To migrate existing controllers to the generic base:

1. **Extract Operations**: Move handler-specific logic into operations struct implementing ComponentOperations  
2. **Remove Protocol Code**: Delete claiming, finalizer, and status management code (handled by base)
3. **Update Controller**: Replace controller implementation with generic base composition
4. **Test**: Verify protocol compliance with existing test suite
5. **Document**: Update handler-specific README with new patterns

See the Helm controller migration as a reference implementation.
