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

### Implementation Support:
- `../deployment-operator/handler/util` - Handler utilities for protocol compliance, finalizer management, and status updates
- `../deployment-operator/handler/simulator` - ComponentHandlerSimulator provides interface specification for component handler teams

## Documentation Hierarchy

- **Architecture docs** (`../deployment-operator/docs/architecture/`): **Primary reference** - detailed specifications for protocol compliance
- **Handler utilities** (`../deployment-operator/handler/util`): **Implementation tools** - standardized functions for protocol compliance
- **Reference implementation** (`../deployment-operator/handler/simulator`): **Implementation patterns** - ComponentHandlerSimulator shows correct handler behavior
- **Handler documentation** (`internal/controller/README.md`): **Complete implementation guide** - detailed examples and patterns for handler development
- **Individual handler docs** (`internal/controller/*/README.md`): **Handler-specific guidance** - technology-specific implementation details

## Development Rules

### Protocol Compliance is Mandatory

- All handler implementations must follow the three core protocols exactly
- Resource claiming, status updates, and finalizer patterns are strictly defined
- State transitions and coordination patterns are protocol-specified
- Use `../deployment-operator/handler/util` package for standardized protocol compliance

### Implementation Requirements

- **Handler Filtering**: Each handler filters Components by `spec.handler` field matching the handler name
- **Finalizer Management**: All handlers must implement handler-specific finalizers for atomic claiming
- **Status Management**: Status updates must follow ComponentPhase enum values and include proper conditions  
- **Error Handling**: Must set Failed states with detailed messages
- **Testing**: Integration tests using envtest for protocol compliance

### Handler Utilities Package

All handlers must use `../deployment-operator/handler/util` package which provides:
- **ClaimingProtocolValidator** - Validates claiming and deletion protocols
- **Finalizer Management** - Standard finalizer patterns and operations  
- **Status Updates** - Consistent Component status management
- **Testing Support** - Integration with ComponentHandlerSimulator

### Multi-Handler Architecture

- **Multiple handlers per project**: This repository contains multiple component handlers (helm, rds, future handlers)
- **Shared controller infrastructure**: All handlers share the same manager and RBAC setup
- **Handler isolation**: Each handler operates independently on its assigned Component resources
- **Consistent patterns**: All handlers follow the same implementation patterns

### Implementation Scope

- **Component handlers only**: Process Component resources and deploy underlying infrastructure
- **Handler-specific logic**: Focus on actual deployment technologies (Terraform, Helm, etc.)
- **Status reporting**: Update Component status to reflect deployment state
- **Cleanup handling**: Respond to coordination finalizer removal for proper cleanup

### Implementation Details

See `internal/controller/README.md` for complete implementation guidance:
- Handler utilities usage examples
- Full controller reconcile loop structure  
- Protocol compliance implementation patterns
- Testing patterns with ComponentHandlerSimulator

## Implementation Workflow

### For Major Implementation Work

0. **Ask for implementation permission**: "Should I implement these changes?" before proceeding with any code modifications
1. **Review architecture docs**: Use `../deployment-operator/docs/architecture/` for protocol specifications and constraints
2. **Focus on Component Handlers**: Implement deployment logic, not orchestration logic
3. **Test protocol compliance**: Validate implementation against ComponentHandlerSimulator patterns

## Current Handlers

- **Helm Handler** (`internal/controller/helm/`) - Deploy and manage Helm charts
- **RDS Handler** (`internal/controller/rds/`) - Deploy and manage RDS instances

See project `README.md` for multi-handler registration and setup details.

## Adding New Handlers

1. Create handler directory: `internal/controller/{handler-name}/`
2. Implement ComponentReconciler following patterns in `internal/controller/README.md`
3. Register controller in `cmd/main.go`
4. Add integration tests validating protocol compliance

Protocol compliance is mandatory - use handler utilities and follow established patterns.

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
