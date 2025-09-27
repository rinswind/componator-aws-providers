# Generic Controller Refactor Implementation Plan

## Overview

Refactor the current Helm controller to separate generic protocol logic from handler-specific operations, enabling code reuse across all Component handlers while maintaining protocol compliance.

## Current State Analysis

**Problem**: The Helm controller (`internal/controller/helm/controller.go`) contains both:

- Generic protocol logic (state machine, finalizer management, status transitions)
- References to Helm-specific operations (deployment, upgrade, deletion functions)

**Goal**: Extract generic protocol logic into a reusable base while keeping handler-specific operations isolated.

## Architecture Target

### Generic Base Controller

- **Purpose**: Handle all protocol state machine logic
- **Location**: `internal/controller/base/`
- **Responsibilities**: Claiming protocol, status transitions, finalizer management, requeue logic
- **Interface**: Accept handler-specific operation implementations via dependency injection

### Handler-Specific Operations Interface

- **Purpose**: Define contract for handler implementations
- **Pattern**: Interface with methods for deploy, check readiness, upgrade, delete, check deletion
- **Implementation**: Each handler (Helm, RDS) implements this interface

### Separation of Concerns

- **Generic**: State transitions, protocol compliance, error handling patterns
- **Handler-Specific**: Technology deployment logic, resource checking, configuration parsing

## Implementation Steps

### Phase 1: Interface Definition

1. **Create operation interface** in `internal/controller/base/operations.go`
   - Define methods for deployment lifecycle operations
   - Use return signatures that support the three-error pattern (success, ioError, businessError)
   - Include context and component parameters

2. **Create generic controller base** in `internal/controller/base/controller.go`
   - Extract protocol state machine from current Helm controller
   - Accept operations interface via dependency injection
   - Maintain exact same protocol compliance behavior
   - Preserve all error handling and requeue patterns

3. **Define configuration interface** for handler-specific settings
   - Requeue periods, timeouts, controller naming
   - Allow handlers to customize behavior without changing protocol logic

### Phase 2: Helm Controller Refactoring

1. **Create Helm operations implementation**
   - Move existing operation functions into new struct that implements interface
   - Keep all existing Helm-specific logic in separate files
   - No changes to actual deployment/deletion logic

2. **Refactor Helm controller** to use generic base
   - Replace current controller logic with composition of generic base + Helm operations
   - Maintain same public interface and RBAC annotations
   - Preserve all existing functionality and behavior

3. **Validate Helm controller functionality**
   - Run existing tests to ensure no regression
   - Verify protocol compliance maintained
   - Check that all edge cases still work

### Phase 3: RDS Controller Implementation

1. **Create RDS operations implementation**
   - Implement the operations interface with RDS-specific logic
   - Follow same patterns as Helm implementation
   - Add proper TODO placeholders for actual RDS deployment logic

2. **Update RDS controller** to use generic base
   - Replace current placeholder implementation
   - Use same composition pattern as Helm controller
   - Add proper protocol compliance

### Phase 4: Validation and Cleanup

1. **Integration testing**
   - Test both controllers with same test scenarios
   - Verify protocol compliance for both handlers
   - Confirm no behavioral changes for Helm controller

2. **Documentation updates**
    - Update controller implementation README with new patterns  
    - Add examples showing how to implement new handlers
    - Document the operations interface contract

## Success Criteria

### Functional Requirements

- **No regression**: Helm controller behavior identical to current implementation
- **Protocol compliance**: Both controllers follow all three core protocols exactly
- **Code reuse**: Generic protocol logic shared between handlers
- **Extensibility**: New handlers can be added by implementing operations interface

### Quality Requirements

- **Test coverage**: All existing tests pass without modification
- **Error handling**: Same error patterns and requeue behavior maintained
- **Logging**: Consistent logging across all handlers
- **Performance**: No performance degradation

## Implementation Constraints

### Protocol Compliance

- **Finalizer patterns**: Must maintain exact same finalizer management
- **Status transitions**: ComponentPhase transitions must be identical
- **Error categorization**: Preserve distinction between I/O errors and business errors
- **Coordination**: Deletion coordination with Composition controller unchanged

### Backward Compatibility

- **Public interfaces**: No changes to controller registration or RBAC
- **Configuration**: Handler-specific configuration patterns preserved
- **Behavior**: All timing, requeue, and error behaviors maintained

### Code Organization

- **File structure**: Handler-specific operations remain in handler directories
- **Import patterns**: No circular dependencies introduced
- **Naming conventions**: Follow existing patterns for controller and handler naming

## Risks and Mitigations

### Risk: Protocol Regression

**Mitigation**: Extensive testing with existing test suite, careful preservation of all protocol logic

### Risk: Interface Too Rigid

**Mitigation**: Start with Helm requirements, then adapt interface based on RDS needs during implementation

### Risk: Complexity Increase

**Mitigation**: Keep interface simple, focus on common patterns rather than edge cases

### Risk: Handler-Specific Edge Cases

**Mitigation**: Allow handlers to extend base behavior through configuration and optional interface methods

## Implementation Notes

### Design Principles

- **Composition over inheritance**: Use dependency injection rather than Go embedding
- **Interface segregation**: Keep operation interface focused on essential methods
- **Single responsibility**: Base handles protocol, operations handle technology
- **Fail-safe**: Default to current behavior when in doubt

### Testing Strategy

- **Unit tests**: Test base controller with mock operations
- **Integration tests**: Use actual Helm operations for integration testing  
- **Regression tests**: Ensure Helm controller passes all existing tests
- **Protocol tests**: Verify state machine transitions remain correct

### Future Extensibility

- **New handlers**: Should only need to implement operations interface
- **Handler variants**: Allow multiple handlers for same technology (e.g., different Helm configurations)
- **Operation extensions**: Interface should support optional methods for advanced handlers
