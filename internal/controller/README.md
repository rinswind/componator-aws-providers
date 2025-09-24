# Component Handler Implementation Guide

This directory contains Component handler controllers that implement specific deployment technologies (Terraform, Helm, etc.) following the deployment orchestration protocols.

## Handler Utilities Usage

All handlers must use the `../deployment-operator/handler/util` package for protocol compliance:

### Protocol Validation

```go
import "github.com/rinswind/deployment-operator/handler/util"

func (r *ComponentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    component := &v1alpha1.Component{}
    if err := r.Get(ctx, req.NamespacedName, component); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    validator := util.NewClaimingProtocolValidator(r.HandlerName)

    // Check if we should process this component
    if validator.ShouldIgnore(component) {
        return ctrl.Result{}, nil
    }

    // Validate claiming eligibility
    if err := validator.CanClaim(component); err != nil {
        return ctrl.Result{}, err
    }

    // Validate deletion eligibility
    if err := validator.CanDelete(component); err != nil {
        return ctrl.Result{RequeueAfter: time.Second * 5}, nil
    }
}
```

### Finalizer Management

```go
// Add handler finalizer during claiming
util.AddHandlerFinalizer(component, "handler-name")

// Check finalizer status
if util.HasHandlerFinalizer(component, "handler-name") {
    // Component is claimed by this handler
}

// Remove finalizer during cleanup
util.RemoveHandlerFinalizer(component, "handler-name")
```

### Status Updates

```go
// Update status through deployment lifecycle
util.SetClaimedStatus(component, "handler-name")
util.SetDeployingStatus(component, "handler-name")
util.SetReadyStatus(component)
util.SetFailedStatus(component, "handler-name", "error description")
util.SetTerminatingStatus(component, "handler-name")

// Check status conditions
if util.IsTerminating(component) {
    // Handle deletion
}
```

## Complete Controller Structure

### Standard Directory Layout

```txt
internal/controller/{handler-name}/
├── controller.go      # Main controller implementation
├── controller_test.go # Unit tests
└── README.md         # Handler-specific documentation
```

### Controller Implementation Pattern

```go
package handlername

import (
    "context"
    "time"

    v1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
    "github.com/rinswind/deployment-operator/handler/util"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/runtime/scheme"
)

type ComponentReconciler struct {
    client.Client
    Scheme *runtime.Scheme
    HandlerName string
}

func (r *ComponentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. Fetch Component
    component := &v1alpha1.Component{}
    if err := r.Get(ctx, req.NamespacedName, component); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // 2. Create protocol validator
    validator := util.NewClaimingProtocolValidator(r.HandlerName)
    
    // 3. Filter by handler name
    if validator.ShouldIgnore(component) {
        return ctrl.Result{}, nil
    }

    // 4. Handle deletion if DeletionTimestamp set
    if util.IsTerminating(component) {
        return r.handleDeletion(ctx, component, validator)
    }

    // 5. Implement claiming protocol and creation/deployment logic
    return r.handleCreation(ctx, component, validator)
}

func (r *ComponentReconciler) handleCreation(ctx context.Context, component *v1alpha1.Component, validator *util.ClaimingProtocolValidator) (ctrl.Result, error) {
    // Check if we can claim
    if err := validator.CanClaim(component); err != nil {
        return ctrl.Result{}, err
    }

    // Claim if not already claimed by us
    if !util.HasHandlerFinalizer(component, r.HandlerName) {
        util.AddHandlerFinalizer(component, r.HandlerName)
        if err := r.Update(ctx, component); err != nil {
            return ctrl.Result{}, err
        }
        util.SetClaimedStatus(component, r.HandlerName)
        return ctrl.Result{}, r.Status().Update(ctx, component)
    }

    // Set deploying status
    if !util.IsPhase(component, v1alpha1.ComponentPhaseDeploying) {
        util.SetDeployingStatus(component, r.HandlerName)
        if err := r.Status().Update(ctx, component); err != nil {
            return ctrl.Result{}, err
        }
    }

    // Implement your deployment logic here
    if err := r.deployResources(ctx, component); err != nil {
        util.SetFailedStatus(component, r.HandlerName, err.Error())
        r.Status().Update(ctx, component)
        return ctrl.Result{}, err
    }

    // Set ready status
    util.SetReadyStatus(component)
    return ctrl.Result{}, r.Status().Update(ctx, component)
}

func (r *ComponentReconciler) handleDeletion(ctx context.Context, component *v1alpha1.Component, validator *util.ClaimingProtocolValidator) (ctrl.Result, error) {
    // Check if we can delete
    if err := validator.CanDelete(component); err != nil {
        // Wait for composition coordination signal
        return ctrl.Result{RequeueAfter: time.Second * 5}, nil
    }

    // Set terminating status
    if !util.IsPhase(component, v1alpha1.ComponentPhaseTerminating) {
        util.SetTerminatingStatus(component, r.HandlerName)
        if err := r.Status().Update(ctx, component); err != nil {
            return ctrl.Result{}, err
        }
    }

    // Perform cleanup
    if err := r.cleanupResources(ctx, component); err != nil {
        util.SetFailedStatus(component, r.HandlerName, err.Error())
        r.Status().Update(ctx, component)
        return ctrl.Result{}, err
    }

    // Remove finalizer to release resource
    util.RemoveHandlerFinalizer(component, r.HandlerName)
    return ctrl.Result{}, r.Update(ctx, component)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&v1alpha1.Component{}).
        Complete(r)
}

// Implement these methods with your handler-specific logic
func (r *ComponentReconciler) deployResources(ctx context.Context, component *v1alpha1.Component) error {
    // Handler-specific deployment logic
    return nil
}

func (r *ComponentReconciler) cleanupResources(ctx context.Context, component *v1alpha1.Component) error {
    // Handler-specific cleanup logic
    return nil
}
```

## Protocol Compliance Requirements

### Core Lifecycle Methods

- **Claiming**: Use handler-specific finalizers for atomic resource discovery
- **Creation**: React immediately when Component resources are created
- **Deployment**: Implement actual infrastructure deployment (Terraform, Helm, etc.)
- **Status Updates**: Report deployment progress through Component status
- **Deletion**: Implement cleanup when coordination finalizer is removed

### Required Patterns

- **Handler Filtering**: Only process Components where `spec.handler` matches handler name using `util.ClaimingProtocolValidator.ShouldIgnore()`
- **Finalizer Management**: Add/remove handler-specific finalizers using `util.AddHandlerFinalizer()` and `util.RemoveHandlerFinalizer()`
- **Status Phases**: Use ComponentPhase enum values (Pending, Claimed, Deploying, Ready, Failed, Terminating)
- **Error Handling**: Set Failed status with descriptive messages using `util.SetFailedStatus()`

## Testing Patterns

### Unit Testing with ComponentHandlerSimulator

```go
import "github.com/rinswind/deployment-operator/handler/simulator"

func TestHandlerLifecycle(t *testing.T) {
    sim := simulator.NewComponentHandlerSimulator("test-handler", testClient)

    // Test claiming protocol
    err := sim.ClaimingProtocol(ctx, component)
    require.NoError(t, err)
    
    // Test deployment phases
    err = sim.Deploy(ctx, component)
    require.NoError(t, err)
    
    err = sim.Ready(ctx, component)
    require.NoError(t, err)
    
    // Test deletion protocol  
    err = sim.DeletionProtocol(ctx, component)
    require.NoError(t, err)
}
```

### Integration Testing

Use envtest for integration tests that validate protocol compliance:

```go
func TestComponentReconciler_Integration(t *testing.T) {
    // Set up envtest environment
    // Create Component resource
    // Verify claiming protocol
    // Verify status transitions
    // Verify deletion protocol
}
```

## Current Handlers

- **helm**: Deploy and manage Helm charts (`internal/controller/helm/`)
- **rds**: Deploy and manage RDS instances (`internal/controller/rds/`)

See individual handler directories for technology-specific implementation details.
