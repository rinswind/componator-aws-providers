# GitHub Copilot Instructions for Deployment Handlers

## Project Context

You are working on **Kubernetes Component Handler Controllers** that implement specific deployment technologies (Terraform, Helm, etc.) for the deployment orchestration system. This project contains multiple controllers that handle different component types through the standardized Component resource interface.

## Project Scope

**This repository implements Component Handler Controllers only.** The Composition Controller and CRD definitions are in the separate deployment-operator project. Component handlers claim and process Component resources based on their `spec.handler` field, implementing the actual deployment logic while following standardized protocols.

### Component Handler Responsibilities:
- Claim Component resources using the claiming protocol with handler-specific finalizers
- Deploy underlying infrastructure (Terraform modules, Helm charts, etc.)
- Report status back through Component resource updates
- Handle cleanup when coordination finalizer is removed
- Implement the handler side of all three core protocols

### Composition Controller Responsibilities (External):
- Create and manage Composition resources
- Create Component resources based on Composition specs
- Aggregate status from Component resources
- Coordinate deletion through finalizer removal

## Critical: Architecture Documentation Reference

**These documents contain authoritative specifications** - reference specific sections for protocol compliance:

### Core Protocol References:
- `../deployment-operator/docs/architecture/claiming-protocol.md` - Atomic resource discovery with handler-specific finalizers
- `../deployment-operator/docs/architecture/creation-protocol.md` - Immediate resource creation and status-driven progression 
- `../deployment-operator/docs/architecture/deletion-protocol.md` - Finalizer-based deletion coordination patterns

### Resource Specification References:
- `../deployment-operator/docs/architecture/component.md` - Individual deployment unit specifications
- `../deployment-operator/docs/architecture/composition.md` - Root orchestrator resource patterns
- `../deployment-operator/docs/architecture/README.md` - System overview and relationships

### Reference Implementation:
- `../deployment-operator/internal/controller/helpers_components_test.go` - ComponentHandlerSimulator provides interface specification for component handler teams

## Development Rules

### 1. Protocol Compliance is Mandatory

- All handler implementations must follow the three core protocols exactly
- Resource claiming, status updates, and finalizer patterns are strictly defined
- State transitions and coordination patterns are protocol-specified

### 2. Component Handler Implementation Focus

- **Implementation only**: Process Component resources and deploy underlying infrastructure
- **Handler-specific logic**: Focus on actual deployment technologies (Terraform, Helm, etc.)
- **Status reporting**: Update Component status to reflect deployment state
- **Cleanup handling**: Respond to coordination finalizer removal for proper cleanup

### 3. Required Implementation Standards

- Each handler filters Components by `spec.handler` field matching the handler name
- All handlers must implement handler-specific finalizers for atomic claiming
- Status updates must include proper Kubernetes conditions and phases
- Error handling must set Failed states with detailed messages
- Integration tests using envtest for protocol compliance

### 4. Multi-Handler Architecture

- **Multiple handlers per project**: This repository contains multiple component handlers (helm, rds, future handlers)
- **Shared controller infrastructure**: All handlers share the same manager and RBAC setup
- **Handler isolation**: Each handler operates independently on its assigned Component resources
- **Consistent patterns**: All handlers should follow the same implementation patterns

## Implementation Workflow

### For Major Implementation Work

0. **Ask for implementation permission**: "Should I implement these changes?" before proceeding with any code modifications
1. **Review architecture docs**: Use `../deployment-operator/docs/architecture/` for protocol specifications and constraints
2. **Focus on Component Handlers**: Implement deployment logic, not orchestration logic
3. **Test protocol compliance**: Validate implementation against ComponentHandlerSimulator patterns

### Documentation Hierarchy

- **Architecture docs** (`../deployment-operator/docs/architecture/`): **Primary reference** - detailed specifications for protocol compliance
- **Reference implementation** (`../deployment-operator/internal/controller/helpers_components_test.go`): **Implementation patterns** - ComponentHandlerSimulator shows correct handler behavior
- **Handler documentation** (`internal/controller/*/README.md`): **Handler-specific guidance** - individual handler implementation details

## Component Handler Interface

All component handlers in this project must implement:

### Core Lifecycle Methods:
- **Claiming**: Use handler-specific finalizers for atomic resource discovery
- **Creation**: React immediately when Component resources are created
- **Deployment**: Implement actual infrastructure deployment (Terraform, Helm, etc.)
- **Status Updates**: Report deployment progress through Component status
- **Deletion**: Implement cleanup when coordination finalizer is removed

### Required Patterns:
- **Handler Filtering**: Only process Components where `spec.handler` matches handler name
- **Finalizer Management**: Add/remove handler-specific finalizers following protocol
- **Status Phases**: Use ComponentPhase enum values (Pending, Claimed, Deploying, Ready, Failed, Terminating)
- **Error Handling**: Set Failed phase with descriptive messages on errors

## Handler Implementation Structure

### Standard Directory Layout:
```
internal/controller/{handler-name}/
├── controller.go      # Main controller implementation
├── controller_test.go # Unit tests
└── README.md         # Handler-specific documentation
```

### Required Controller Structure:
```go
type ComponentReconciler struct {
    client.Client
    Scheme *runtime.Scheme
}

func (r *ComponentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. Fetch Component
    // 2. Filter by handler name
    // 3. Handle deletion if DeletionTimestamp set
    // 4. Implement claiming protocol
    // 5. Implement creation/deployment logic
    // 6. Update status appropriately
}
```

### Registration in main.go:
```go
// Register each handler controller
if err := (&handlername.ComponentReconciler{
    Client: mgr.GetClient(),
    Scheme: mgr.GetScheme(),
}).SetupWithManager(mgr); err != nil {
    setupLog.Error(err, "unable to create controller", "controller", "HandlerName")
    os.Exit(1)
}
```

## Current Handlers

### Helm Controller (`internal/controller/helm/`)
- **Handler Name**: `"helm"`
- **Purpose**: Deploy and manage Helm charts
- **Technology**: Helm 3 client libraries
- **Resources**: Helm releases, ConfigMaps, Secrets

### RDS Controller (`internal/controller/rds/`)
- **Handler Name**: `"rds"`
- **Purpose**: Deploy and manage RDS instances
- **Technology**: Terraform providers
- **Resources**: RDS instances, security groups, subnets

## Adding New Handlers

### Implementation Steps:
1. **Create handler directory**: `internal/controller/{handler-name}/`
2. **Implement ComponentReconciler**: Follow standard controller structure
3. **Add unit tests**: Test claiming, deployment, and deletion protocols
4. **Add README**: Document handler-specific behavior
5. **Register in main.go**: Add controller setup
6. **Update RBAC**: Add required permissions if needed

### Protocol Compliance Checklist:
- [ ] Handler filters Components by `spec.handler` field
- [ ] Implements claiming protocol with handler-specific finalizer
- [ ] Reacts immediately to Component creation
- [ ] Updates status through all deployment phases
- [ ] Handles deletion coordination with composition finalizer
- [ ] Sets Failed status with descriptive messages on errors
- [ ] Includes integration tests validating protocol compliance

## Common Tasks Reference

- **Adding Handlers**: Follow handler implementation structure and protocol patterns
- **Testing Handlers**: Use ComponentHandlerSimulator patterns for protocol compliance
- **Debugging Issues**: Check Component status phases and finalizer coordination
- **Protocol Compliance**: Validate against architecture specifications and reference implementation
- **Status Updates**: Follow ComponentPhase enum and condition patterns

## External Dependencies

This project depends on:
- **deployment-operator**: CRD definitions and API types
- **Handler-specific technologies**: Terraform, Helm, etc.
- **Kubernetes controller-runtime**: Core controller infrastructure

The dependency on deployment-operator is managed via Go modules with a local replace directive for development.
