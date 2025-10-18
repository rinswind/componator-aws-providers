# Helm Handler Automatic Rollback Support Architecture

## Overview

This document specifies the architecture for automatic rollback support in the Helm handler. This enhancement provides automatic rollback capabilities as error recovery when deployments fail, using Helm's native rollback functionality while maintaining protocol compliance with the Component state machine.

### Implementation Status

**Current Recommendation**: Implementation of this feature is **not recommended** for production use based on empirical research of Helm rollback reliability issues.

**Research Findings**: Investigation of Helm GitHub issues revealed systematic reliability problems that make automatic rollback unsuitable as error recovery mechanism:

- **Three-Way Merge State Corruption**: Rollbacks fail to restore original state when upgrades fail mid-deployment. Resources added during failed upgrades become "sticky" and persist after rollback due to three-way strategic merge patches using the last successful release as base while ignoring failed attempts.

- **Resource Policy Annotation Failures**: `helm.sh/resource-policy` annotations cause rollbacks to fail 100% of the time on first attempt with "resource not found" errors, even when resources exist in the cluster. This indicates fundamental state tracking problems between Helm's knowledge and cluster reality.

- **Resource Tracking Inconsistencies**: When upgrades fail before deleting resources, rollbacks fail with "no resource found" errors despite valid cluster state. Helm cannot match existing cluster resources to stored manifests, completely blocking rollback execution.

**Success Context Analysis**: Limited success stories involve manual intervention, immediate execution, and simple configurations - none of which apply to automatic rollback as error recovery in complex distributed systems.

**Design Preservation**: This complete architecture specification is maintained for potential future implementation should Helm rollback reliability improve, or for adaptation to alternative rollback mechanisms that address the identified systematic issues.

## Current Implementation Context

**Production Status**: The Helm handler is fully operational with:

- Chart installation and uninstallation lifecycle management
- Values override through Component configuration
- Complete protocol compliance with claiming, creation, and deletion patterns
- Robust status reporting and error handling

**Architecture Foundation**: Built on the factory pattern with `HelmOperationsFactory` and stateful `HelmOperations` instances that maintain parsed configuration throughout reconciliation loops.

## Automatic Rollback Support

**Objective**: Provide automatic rollback as error recovery mechanism when deployments fail.

**Approach**: When deployment operations fail, automatically attempt rollback to previous stable revision to maintain system stability, then report deployment failure through Failed state.

**Configuration Schema**:

```yaml
source:
  chart:
    version: "1.2.0"
autoRollback: true  # Optional: enable automatic rollback on deployment failure
```

**Error Recovery Logic**:

- Handler attempts install (no existing release) or upgrade (existing release) based on release existence
- On deployment failure, if `autoRollback: true`, automatically rollback to previous revision
- Always transition to Failed state when deployment attempt fails (regardless of rollback success)
- Rollback provides stability - prevents leaving system in broken state

**Optional Handler Feature**:

- Automatic rollback support is optional, similar to timeout functionality
- Handlers that don't implement rollback simply ignore the `autoRollback` configuration
- Simple handlers remain simple without forced rollback complexity
- Advanced handlers can implement sophisticated error recovery

## Configuration Architecture

### Automatic Rollback Configuration Structure

**Updated Configuration Structure**: The existing `HelmConfig` struct will be extended to support automatic rollback:

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
    
    // Optional automatic rollback on deployment failure
    AutoRollback *bool `json:"autoRollback,omitempty"`
}
```

### Optional Handler Feature Pattern

**Similar to Timeout Support**: Automatic rollback follows the same optional implementation pattern as timeouts:

- **Optional Implementation**: Rollback support is not required for all handlers
- **Handler Autonomy**: Each handler decides whether to implement automatic rollback functionality
- **Configuration Handling**: Handlers that don't support rollback can ignore the `autoRollback` field
- **Simple Handlers Stay Simple**: Basic handlers remain straightforward without forced rollback complexity

**Handler Examples**:

- **helm-webapp**: Implements automatic rollback due to complex application deployments
- **terraform-rds**: May implement rollback due to long provisioning times and failure recovery needs
- **kubectl-manifests**: Likely doesn't implement rollback due to fast, simple operations

## Automatic Rollback Operations Interface

### Rollback Logic

**Error Recovery Flow**:

- Record current Helm revision before attempting deployment operation
- Attempt install (no existing release) or upgrade (existing release) based on release existence
- On deployment failure, if `autoRollback: true`, initiate rollback to recorded revision
- Transition to RollingBack state during rollback operation (requires Component state machine extension)
- Always transition to Failed state when rollback completes (regardless of rollback success/failure)

**Rollback Implementation**:

- **Revision Tracking**: Record current revision before deployment attempts
- **Async Operations**: Handle rollback as async operation requiring state tracking
- **Status Integration**: Report rollback progress and results through Component status conditions
- **State Machine Integration**: Requires RollingBack state for proper async operation tracking

**State Transitions for Rollback**:

- **Deploying → RollingBack**: Deployment failed, rollback initiated
- **RollingBack → Failed**: Rollback completed (success or failure both go to Failed)
- **Failed State Message**: Includes rollback information for user visibility

## Implementation Strategy

### Development Phases

#### Phase 1: Downgrade Strategy Configuration

**File Changes Required**:

- `config.go`: Add `DowngradeStrategy` string field to `HelmConfig` struct
- `config.go`: Add validation for downgrade strategy values

**Implementation Steps**:

1. Add downgrade strategy field to `HelmConfig`
2. Add validation for "rollback" and "reinstall" strategy values
3. Extend configuration parsing to handle downgrade strategy

#### Phase 2: Version Detection and Strategy Implementation

**File Changes Required**:

- `operations.go`: Add version comparison utilities
- New file `operations_rollback.go`: Rollback and downgrade strategy logic
- `operations_deploy.go`: Add version detection in deployment operations

**Implementation Steps**:

1. Implement chart version comparison logic (semantic versioning)
2. Add version downgrade detection during deployment operations
3. Implement rollback strategy (Helm rollback to target version)
4. Implement reinstall strategy (install older version as new release)
5. Integrate strategy selection with deployment flow

#### Phase 3: Controller Integration

**File Changes Required**:

- `controller.go`: Integrate rollback operations into reconciliation logic
- `operations.go`: Extend status reporting for rollback operations

**Implementation Steps**:

1. Integrate version-aware deployment logic into reconciliation
2. Add comprehensive error handling for both rollback and reinstall strategies
3. Add testing scenarios for version upgrades and downgrades

### Implementation Sequence

**Recommended Order**: Implement phases sequentially to build version-aware downgrade capabilities:

1. Downgrade Strategy Configuration (enables strategy configuration)
2. Version Detection and Strategy Implementation (core version-aware functionality)
3. Controller Integration (complete downgrade support)

**Key Implementation Files**:

- `internal/controller/helm/config.go` - Downgrade strategy configuration
- `internal/controller/helm/operations.go` - Version comparison utilities
- `internal/controller/helm/operations_rollback.go` - Version-aware rollback implementation (new)
- `internal/controller/helm/operations_deploy.go` - Version detection integration
- `internal/controller/helm/controller.go` - Controller integration

## Architectural Constraints

### Component State Machine Compliance

**State Transition Requirements**: Rollback operations must comply with Component state machine:

- **Failed → Deploying**: Only triggered by Component spec changes (dirty detection)
- **Version Changes**: Chart version or downgrade strategy changes make Component "dirty"
- **Recovery Principle**: External intervention (spec modification) required for recovery from Failed state

**Implementation Approach**:

- Chart version or downgrade strategy changes make Component "dirty" (`component.Generation != observedGeneration`)
- Standard Failed → Deploying transition occurs per existing state machine
- Version-aware logic executes during deployment phase, selecting appropriate strategy

### Protocol Compliance

**Existing Requirements**: Rollback enhancements must maintain compliance with the three core protocols:

- **Claiming Protocol**: Handler-specific finalizers and atomic resource discovery
- **Creation Protocol**: Status-driven progression with proper condition reporting
- **Deletion Protocol**: Coordinated cleanup through finalizer management

**Rollback Integration**: Rollback operations integrate with existing protocol patterns without disrupting normal deployment workflows.

### Operational Reliability

**Error Handling**: Downgrade operations provide clear error reporting for rollback failures and validation errors
**Status Reporting**: Downgrade operations integrate with existing Component status conditions and phase reporting
**Recovery Patterns**: Downgrade features support graceful recovery from rollback failures through standard retry mechanisms

### Usage Pattern

**Automatic Rollback Workflow**:

1. Component has deployed chart version 1.2.0, currently in Ready state
2. User requests upgrade to version 1.3.0 with automatic rollback enabled
3. User updates Component spec: chart version → 1.3.0, autoRollback → true
4. Component becomes "dirty", triggers Ready → Deploying transition per state machine
5. Helm handler records current revision (e.g., revision 2) before attempting upgrade
6. Handler attempts upgrade to version 1.3.0, upgrade fails
7. Handler initiates rollback to revision 2, transitions Component to RollingBack state
8. Handler polls rollback status on subsequent reconciles
9. When rollback completes, Component transitions to Failed state with rollback information

**Configuration Examples**:

```yaml
# Enable automatic rollback on deployment failure
source:
  chart:
    version: "1.3.0"
autoRollback: true

# Disable automatic rollback (default)
source:
  chart:
    version: "1.3.0"
autoRollback: false
```

**Status Communication Examples**:

**Successful rollback**:

```yaml
status:
  phase: Failed
  message: "Chart upgrade to v1.3.0 failed: timeout exceeded. Successfully rolled back to revision 2 (v1.2.0)."
```

**Failed rollback**:

```yaml
status:
  phase: Failed
  message: "Chart upgrade to v1.3.0 failed: timeout exceeded. Rollback to revision 2 also failed: release not found."
```

### Helm Integration

**Release History Dependency**: Downgrade operations use Helm's native release history tracking
**State Consistency**: Downgrade operations maintain consistency between Component status and actual Helm release state
