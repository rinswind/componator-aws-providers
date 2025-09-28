# Helm Handler Rollback Support Architecture

## Overview

This document specifies the architecture for rollback operation support in the Helm handler. These enhancements provide controlled rollback capabilities for failed deployments or version downgrades while maintaining protocol compliance and production stability.

## Current Implementation Context

**Production Status**: The Helm handler is fully operational with:

- Chart installation and uninstallation lifecycle management
- Values override through Component configuration
- Complete protocol compliance with claiming, creation, and deletion patterns
- Robust status reporting and error handling

**Architecture Foundation**: Built on the factory pattern with `HelmOperationsFactory` and stateful `HelmOperations` instances that maintain parsed configuration throughout reconciliation loops.

## Rollback Operation Support

**Objective**: Provide controlled rollback capabilities for failed deployments or version downgrades.

**Conceptual Analysis**:
Helm rollback operates on release history, reverting to previous successful releases rather than installing older chart versions. This differs from version downgrades, which install older chart versions as new releases.

**Configuration Schema Extension**:

```yaml
operations:
  rollback:
    enabled: true
    targetRevision: 2
    strategy: automatic
```

**Implementation Patterns**:

- **Rollback Detection**: Identify scenarios where rollback is appropriate (deployment failures, explicit version downgrades)
- **History Management**: Maintain awareness of release revision history through Helm's native tracking
- **Rollback Execution**: Implement rollback operations using Helm's rollback action with proper validation
- **Status Integration**: Report rollback operations through Component status conditions

**Rollback Strategies**:

- **Automatic Rollback**: Automatically rollback failed upgrades to last known good state
- **Manual Rollback**: Support explicit rollback requests through Component configuration
- **Version-Based Rollback**: Interpret older chart versions as rollback requests when release history exists

**Operational Considerations**:

- **History Retention**: Configure release history retention policies to support rollback capabilities
- **State Validation**: Verify rollback target validity before execution
- **Coordination**: Ensure rollback operations maintain protocol compliance with status reporting

## Configuration Architecture

### Rollback Configuration Structure

**Updated Configuration Structure**: The existing `HelmConfig` struct will be extended to support rollback operations:

```go
type HelmConfig struct {
    // Existing common fields
    ReleaseName      string                 `json:"releaseName" validate:"required"`
    ReleaseNamespace string                 `json:"releaseNamespace" validate:"required"`
    ManageNamespace  *bool                  `json:"manageNamespace,omitempty"`
    Values           map[string]interface{} `json:"values,omitempty"`
    Timeouts         *HelmTimeouts          `json:"timeouts,omitempty"`
    
    // Source configuration (from chart sources architecture)
    Source SourceConfig `json:"source" validate:"required"`
    
    // Optional rollback configuration
    Operations *HelmOperations `json:"operations,omitempty"`
}

type HelmOperations struct {
    Rollback *RollbackConfig `json:"rollback,omitempty"`
}

type RollbackConfig struct {
    Enabled        bool   `json:"enabled,omitempty"`
    TargetRevision int    `json:"targetRevision,omitempty"`
    Strategy       string `json:"strategy,omitempty" validate:"oneof=automatic manual"`
}
```

### Rollback Strategy Specifications

**Automatic Strategy**:
- Triggers rollback automatically when deployment failures are detected
- Uses last known successful revision as rollback target
- Integrates with Component status conditions for failure detection

**Manual Strategy**:
- Requires explicit configuration of target revision
- Validates target revision exists in release history
- Provides controlled rollback execution with user oversight

**Version-Based Strategy**:
- Interprets chart version downgrades as rollback requests
- Compares current chart version with requested version
- Falls back to revision-based rollback when version history is available

## Rollback Operations Interface

### Enhanced Operations Interface

```go
// ComponentOperations interface extension for rollback support
type ComponentOperations interface {
    // Existing operations
    Deploy(ctx context.Context) error
    Upgrade(ctx context.Context) error
    Delete(ctx context.Context) error
    GetStatus(ctx context.Context) (ComponentStatus, error)
    
    // New rollback operation
    Rollback(ctx context.Context) error
    
    // Rollback support operations
    GetReleaseHistory(ctx context.Context) ([]ReleaseRevision, error)
    ValidateRollbackTarget(ctx context.Context, revision int) error
}

type ReleaseRevision struct {
    Revision    int       `json:"revision"`
    ChartVersion string   `json:"chartVersion"`
    Status      string    `json:"status"`
    DeployedAt  time.Time `json:"deployedAt"`
    Description string    `json:"description"`
}
```

### Rollback Detection Logic

**Failure-Based Rollback Detection**:
- Monitor Component status conditions for deployment failures
- Detect timeout conditions in deployment operations
- Identify resource creation or readiness failures
- Trigger automatic rollback when configured strategy permits

**Version-Based Rollback Detection**:
- Compare requested chart version with currently deployed version
- Identify semantic version downgrades (e.g., 1.3.0 → 1.2.0)
- Check release history for target version availability
- Execute rollback to matching historical revision

**Manual Rollback Triggers**:
- Process explicit rollback requests through configuration updates
- Validate target revision specification against release history
- Execute rollback with comprehensive status reporting

## Implementation Strategy

### Development Phases

#### Phase 1: Rollback Configuration Foundation

**File Changes Required**:

- `config.go`: Add rollback configuration structures to `HelmConfig`
- `config.go`: Extend configuration parsing to handle rollback options
- `operations.go`: Add `Rollback()` method to `HelmOperations` interface
- `operations.go`: Add rollback support methods to operations interface

**Implementation Steps**:

1. Define rollback configuration structures with validation
2. Extend configuration parsing to handle rollback options
3. Add rollback method to operations interface
4. Implement release history management utilities
5. Add rollback target validation logic

#### Phase 2: Rollback Detection and Strategy Implementation

**File Changes Required**:

- New file `operations_rollback.go`: Rollback detection and execution logic
- `controller.go`: Integrate rollback operations into reconciliation logic
- `operations_deploy.go`: Add rollback triggers to deployment failure paths
- `operations.go`: Extend status reporting for rollback operations

**Implementation Steps**:

1. Implement rollback detection based on release history analysis
2. Add failure-based rollback triggers to deployment operations
3. Implement version-based rollback detection logic
4. Add rollback execution using Helm's rollback action with proper validation
5. Integrate rollback status reporting with Component conditions

#### Phase 3: History Management and Validation

**File Changes Required**:

- `operations_rollback.go`: Release history management and validation
- `operations.go`: History retention policy implementation
- `config.go`: Add history retention configuration options
- `controller.go`: Integrate history cleanup with Component lifecycle

**Implementation Steps**:

1. Implement release history retrieval and management
2. Add rollback target validation against available revisions
3. Implement history retention policies with configurable limits
4. Add comprehensive error handling for rollback validation failures
5. Integrate history cleanup with Component deletion operations

#### Phase 4: Advanced Rollback Features

**File Changes Required**:

- `operations_rollback.go`: Advanced rollback strategies and automation
- `controller.go`: Enhanced rollback integration with reconciliation loops
- `operations.go`: Rollback metrics and monitoring integration
- Configuration updates for advanced rollback options

**Implementation Steps**:

1. Implement advanced rollback strategies (automatic, manual, version-based)
2. Add rollback automation triggers based on deployment health monitoring
3. Implement rollback metrics and observability features
4. Add comprehensive rollback testing and validation scenarios
5. Document rollback operation patterns and troubleshooting guidance

### Implementation Sequence

**Recommended Order**: Implement phases sequentially to build rollback capabilities incrementally:

1. Rollback Configuration Foundation (enables basic rollback support)
2. Rollback Detection and Strategy Implementation (core rollback functionality)
3. History Management and Validation (robust rollback operations)
4. Advanced Rollback Features (production-ready rollback automation)

**Key Implementation Files**:

- `internal/controller/helm/config.go` - Rollback configuration extensions
- `internal/controller/helm/operations.go` - Operations interface extensions
- `internal/controller/helm/operations_rollback.go` - Rollback operations implementation (new)
- `internal/controller/helm/controller.go` - Controller integration for rollback operations

## Architectural Constraints

### Protocol Compliance

**Existing Requirements**: All rollback enhancements must maintain compliance with the three core protocols:

- **Claiming Protocol**: Handler-specific finalizers and atomic resource discovery
- **Creation Protocol**: Status-driven progression with proper condition reporting
- **Deletion Protocol**: Coordinated cleanup through finalizer management

**Rollback Integration**: Rollback operations must integrate seamlessly with existing protocol patterns without disrupting normal deployment workflows.

### Operational Reliability

**Error Handling**: Rollback operations must provide clear error categorization between rollback failures, validation errors, and operational issues
**Status Reporting**: Rollback operations must integrate with existing Component status conditions and phase reporting
**Recovery Patterns**: Rollback features must support graceful recovery from rollback failures without compromising system stability

### Helm Integration Constraints

**Release History Dependency**: Rollback operations depend on Helm's native release history tracking and retention policies
**Revision Validation**: All rollback operations must validate target revisions against available Helm release history
**State Consistency**: Rollback operations must maintain consistency between Component status and actual Helm release state
