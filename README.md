# Deployment Handlers

This project contains Kubernetes controllers that handle specific component types for the deployment orchestration system.

## Architecture

- **deployment-operator**: Contains the CRD definitions (`Component`, `Composition`) and the orchestration controller
- **deployment-handlers**: Contains handler-specific controllers that implement the actual deployment logic

## Controllers

This project implements multiple controllers for different component handlers, each organized in its own subpackage:

### Helm Controller (`helm.ComponentReconciler`)
- **Package**: `internal/controller/helm`
- Handles `Component` resources with `spec.handler: "helm"`
- Responsible for deploying and managing Helm charts
- Location: `internal/controller/helm/controller.go`

### RDS Controller (`rds.ComponentReconciler`)
- **Package**: `internal/controller/rds`
- Handles `Component` resources with `spec.handler: "rds"`
- Responsible for deploying and managing RDS instances via AWS RDS SDK
- Location: `internal/controller/rds/controller.go`

## Usage

Each controller filters `Component` resources based on the `spec.handler` field and only processes those assigned to its specific handler type.

### Building and Running

```bash
# Build the project
go build -o bin/manager cmd/main.go

# Run locally (requires kubeconfig)
./bin/manager

# Run with specific flags
./bin/manager --metrics-bind-address=:8080 --health-probe-bind-address=:8081
```

### Development

```bash
# Run tests
make test

# Install CRDs (requires the deployment-operator CRDs to be installed first)
make install

# Run locally
make run
```

## Dependencies

This project depends on the CRD definitions from `deployment-operator`. The dependency is managed via Go modules with a local replace directive:

```go
// In go.mod
require github.com/rinswind/deployment-operator v0.0.0
replace github.com/rinswind/deployment-operator => ../deployment-operator
```

## Handler Implementation

To add a new handler:

1. **Create a new controller package** (e.g., `internal/controller/newhandler/`)
2. **Implement the controller** in `controller.go`
3. **Add tests** in `controller_test.go`
4. **Register the controller** in `cmd/main.go`
5. **Add appropriate RBAC permissions**

Example directory structure for a new handler:
```
internal/controller/newhandler/
├── controller.go      # Main controller implementation
├── controller_test.go # Unit tests
└── README.md         # Handler-specific documentation
```

Example controller structure:
```go
package newhandler

import (
    "context"
    deploymentsv1alpha1 "github.com/rinswind/deployment-operator/api/v1alpha1"
    // other imports...
)

type ComponentReconciler struct {
    client.Client
    Scheme *runtime.Scheme
}

func (r *ComponentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // Fetch the Component
    var component deploymentsv1alpha1.Component
    if err := r.Get(ctx, req.NamespacedName, &component); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // Only process components for this handler
    if component.Spec.Handler != "newhandler" {
        return ctrl.Result{}, nil
    }

    // Implement handler-specific logic here
    return ctrl.Result{}, nil
}
```

Then register in `cmd/main.go`:
```go
import "github.com/rinswind/deployment-handlers/internal/controller/newhandler"

// In main():
if err := (&newhandler.ComponentReconciler{
    Client: mgr.GetClient(),
    Scheme: mgr.GetScheme(),
}).SetupWithManager(mgr); err != nil {
    setupLog.Error(err, "unable to create controller", "controller", "NewHandler")
    os.Exit(1)
}
```