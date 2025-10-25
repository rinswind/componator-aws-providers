# Deployment Handlers

Kubernetes Component Handler Controllers that implement specific deployment technologies (Terraform, Helm, etc.) for the deployment orchestration system.

## Project Overview

This project contains multiple controllers that handle different component types through the standardized Component resource interface:

- **Helm Handler** (`internal/controller/helm/`) - Deploy and manage Helm charts
- **RDS Handler** (`internal/controller/rds/`) - Deploy and manage RDS instances via Terraform
- **Config-Reader Handler** (`internal/controller/configreader/`) - Read ConfigMaps and export values for template resolution

Each handler claims and processes Component resources based on their `spec.handler` field, implementing the actual deployment logic while following standardized protocols and enhanced orchestration capabilities.

## Architecture

- **deployment-operator**: Provides Composition Controller, CRD definitions, and complete handler toolkit (`componentkit/controller/`, `componentkit/util/`, `componentkit/simulator/`)
- **deployment-handlers**: Component Handler Controllers that depend on deployment-operator for protocol infrastructure

**Dependency Direction**: This project imports the base controller and utilities from deployment-operator, enabling external teams to access the complete handler toolkit from a single dependency.

**Enhanced Orchestration**: All handlers support advanced orchestration features including TerminationFailed state handling, timeout compliance, and handler status coordination for production deployments.

## Multi-Handler Registration

All handlers are registered in `cmd/main.go`:

```go
// Register Helm handler
if err := helm.NewComponentReconciler().SetupWithManager(mgr); err != nil {
    setupLog.Error(err, "unable to create controller", "controller", "Helm")
    os.Exit(1)
}

// Register RDS handler  
if err := rds.NewComponentReconciler().SetupWithManager(mgr); err != nil {
    setupLog.Error(err, "unable to create controller", "controller", "RDS")
    os.Exit(1)
}

// Register Config-Reader handler
if err := configreader.NewComponentReconciler(mgr).SetupWithManager(mgr); err != nil {
    setupLog.Error(err, "unable to create controller", "controller", "ConfigReader")
    os.Exit(1)
}
```

## Building and Running

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
require github.com/rinswind/componator v0.0.0
replace github.com/rinswind/componator => ../deployment-operator
```

## Adding New Handlers

To add a new handler:

1. Create controller package in `internal/controller/{handler-name}/`
2. Implement controller following patterns in `internal/controller/README.md`
3. Register controller in `cmd/main.go`
4. Add appropriate RBAC permissions

See `internal/controller/README.md` for detailed implementation guidance and examples.
