# Helm Handler MVP Implementation Plan

## MVP Scope Definition

Implement minimum viable Helm handler with complete protocol compliance and basic chart deployment functionality. Focus on atomic operations, proper error handling, and integration with existing controller patterns.

### Required Functionality

**Helm Operations**:

- Install charts from public repositories
- Uninstall releases during cleanup
- Basic values override through Component.Spec.Config
- Status reporting through Component.Status.Phase

**Configuration Support**:

- Chart repository URL, name, version specification
- Key-value pairs for chart values override
- Target namespace specification
- Deterministic release name generation

### Excluded from MVP

**Advanced Operations**:

- Chart upgrades, rollbacks, history management
- Complex nested values or templating
- Local chart development or custom repositories
- Multi-chart deployments or dependencies

**Production Features**:

- Advanced retry mechanisms or circuit breakers
- Performance optimizations or resource limiting
- Comprehensive observability or metrics collection

## Protocol Requirements and References

**Claiming Protocol**:

- Filter Components by spec.handler == "helm"
- Add finalizer "helm.deployment-orchestrator.io/lifecycle" for claiming
- Skip Components with existing handler finalizers from other controllers
- Handle DeletionTimestamp by routing to deletion logic

**Creation Protocol**:

- Implement Component lifecycle state machine: Claimed → Deploying → Ready/Failed
- No automatic retries - external operator handles failure recovery
- Status updates must not interfere with finalizer operations

**Deletion Protocol**:

- Wait for composition.deployment-orchestrator.io/coordination finalizer removal
- Update status to Terminating when cleanup begins
- Execute Helm release uninstallation
- Remove helm.deployment-orchestrator.io/lifecycle finalizer when complete

**Reference Implementation**: Use ComponentHandlerSimulator patterns from `../deployment-operator/internal/controller/helpers_components_test.go`

**Protocol Specifications**:

- Claiming Protocol: `../deployment-operator/docs/architecture/claiming-protocol.md`
- Creation Protocol: `../deployment-operator/docs/architecture/creation-protocol.md`
- Deletion Protocol: `../deployment-operator/docs/architecture/deletion-protocol.md`
- Component Resource: `../deployment-operator/docs/architecture/component.md`

## Implementation Tasks

### Task 1: Add Helm Dependencies ⚠️ NEEDS RE-IMPLEMENTATION

Add Helm v3 client libraries to go.mod:

- ⚠️ helm.sh/helm/v3/pkg/action
- ⚠️ helm.sh/helm/v3/pkg/chart/loader  
- ⚠️ helm.sh/helm/v3/pkg/cli
- ⚠️ helm.sh/helm/v3/pkg/getter
- ⚠️ helm.sh/helm/v3/pkg/repo

**Implementation Status**: Dependencies were initially added but removed by `go mod tidy` since they weren't being used in code. Will be re-added as part of Task 4 (Helm Client Integration) when actually imported and used.

### Task 2: Define Configuration Schema ✅ COMPLETED

Create Go struct for Component.Spec.Config unmarshaling:

- ✅ Chart reference with repository, name, version fields
- ✅ Values map for string key-value overrides
- ✅ Optional namespace field for target deployment
- ✅ JSON tags for proper unmarshaling

**Implementation Status**: Configuration schema implemented with HelmConfig struct including:
- HelmRepository struct with URL and Name fields
- HelmChart struct with Name and Version fields  
- Values map for chart values override
- Optional Namespace field
- parseHelmConfig() method for JSON unmarshaling from Component.Spec.Config
- generateReleaseName() method for deterministic release naming
- Comprehensive test coverage for configuration parsing and validation
- Updated README with detailed configuration examples and schema documentation

Component.Spec.Config unmarshals to structured HelmConfig enabling chart installation with repository URL, chart coordinates, and values override. Supports both minimal configuration (repository + chart) and full configuration with values and namespace override.

### Task 3: Implement Claiming Protocol ✅ COMPLETED

Follow claiming protocol specification from Protocol Requirements section.

**Implementation Status**: Claiming protocol successfully implemented with full test coverage:

**Core Protocol Implementation**:

- ✅ Handler filtering for `spec.handler == "helm"` components only
- ✅ **Resource Discovery Phase**: SetupWithManager uses predicates to only watch Components with handler "helm"
- ✅ Atomic claiming using `helm.deployment-orchestrator.io/lifecycle` finalizer
- ✅ Conflict detection preventing claims on components with other handler finalizers
- ✅ Proper deletion timestamp handling with `handleDeletion()` method
- ✅ Status updates for claiming (Claimed phase, claimedBy, claimedAt fields)

**Finalizer Management**:

- ✅ `hasAnyHandlerFinalizer()` method detecting existing claims by other handlers
- ✅ `isClaimedByUs()` method checking ownership by helm handler
- ✅ `claimComponent()` atomic claiming operation with finalizer addition
- ✅ Proper finalizer removal during cleanup in deletion protocol

**Deletion Protocol Coordination**:

- ✅ Wait for composition coordination finalizer removal before cleanup
- ✅ Update status to Terminating during cleanup phase
- ✅ Remove handler finalizer to complete deletion process
- ✅ Proper dual-finalizer coordination pattern implementation

**Resource Discovery Optimization**:

- ✅ **SetupWithManager implements Resource Discovery Phase** per claiming protocol
- ✅ Predicate filtering ensures controller only watches Components with `spec.handler == "helm"`
- ✅ Reduces unnecessary reconciliation events for non-helm Components
- ✅ Consistent pattern applied to both helm and rds controllers

**Test Coverage**:

- ✅ **24 comprehensive test cases** covering all protocol scenarios
- ✅ **Claiming Protocol Tests**: Unclaimed component claiming, conflict detection, already-owned handling
- ✅ **Deletion Protocol Tests**: Coordination finalizer waiting, cleanup execution
- ✅ **Error Handling Tests**: Invalid configuration handling with proper status updates
- ✅ **Handler Filtering Tests**: Non-helm component rejection, component not found handling
- ✅ **Resource Discovery Tests**: Predicate filtering validation for different handler types

**Architecture Compliance**:

- ✅ Follows ComponentHandlerSimulator patterns from reference implementation
- ✅ Uses same finalizer naming convention (`{handler}.deployment-orchestrator.io/lifecycle`)
- ✅ Implements proper status state transitions (Pending → Claimed → Failed)
- ✅ Handles atomic operations with Kubernetes conflict resolution
- ✅ Proper separation of claiming, configuration parsing, and deployment logic
- ✅ **Resource Discovery Phase** implementation per claiming protocol specification

The claiming protocol implementation is production-ready and fully compliant with the deployment-orchestrator architecture specifications.

### Task 4: Implement Helm Client Integration

Create Helm client wrapper implementing:

- Chart installation from repository URLs
- Release uninstallation for cleanup
- Release status queries for health monitoring
- Repository configuration and chart loading

**Technical Specifications**:

- Use action.Configuration for Helm operations
- Implement proper Kubernetes client integration
- Generate deterministic release names from Component metadata
- Handle network errors and chart loading failures

### Task 5: Implement Creation Protocol

Implement Component lifecycle state machine from Protocol Requirements section:

- **Claimed**: Component claimed, ready for deployment
- **Deploying**: Helm installation in progress
- **Ready**: Installation complete and verified
- **Failed**: Installation failed with error details

**State Transitions**:

- Claimed Components transition to Deploying with Helm installation start
- Deploying Components check installation completion and transition to Ready
- Failed state captures errors with descriptive messages

### Task 6: Implement Deletion Protocol

Follow deletion protocol coordination from Protocol Requirements section.

### Task 7: Implement Status Management

Update Component.Status fields accurately:

- Phase field reflects current lifecycle state
- ClaimedBy set to "helm" on claiming
- ClaimedAt timestamp on successful claim
- ReadyAt timestamp on deployment completion
- Message field for human-readable status

Use separate status subresource updates.

### Task 8: Add Integration Tests ✅ COMPLETED (Infrastructure)

Test protocol compliance and Helm functionality:

**Protocol Tests**:

- Claiming protocol with finalizer management
- Handler filtering for non-helm Components
- Deletion coordination with composition finalizer
- Multiple controller instance conflict prevention

**Functionality Tests**:

- Chart installation from public repository
- Values override application
- Release cleanup on deletion
- Error scenario handling

**Test Infrastructure**: ✅ COMPLETED

- ✅ Use envtest framework from existing test suite
- ✅ Follow test patterns from other handler implementations
- ✅ Mock external dependencies where appropriate
- ✅ Validate against ComponentHandlerSimulator patterns

**Implementation Status**: Test infrastructure setup completed with envtest integration:

- Both helm and rds controllers now use proper envtest environments instead of fake clients
- Each controller subpackage has independent test suite with CRD support
- Tests can now validate real Kubernetes API behavior including finalizers and status updates
- Foundation ready for implementing protocol compliance tests as outlined in MVP requirements
- All existing tests pass (17 helm specs, 2 rds specs)
- envtest binaries properly configured for CI/CD integration

## Validation Criteria

**Protocol Compliance**:

- All three protocols implemented according to architecture specifications
- ComponentHandlerSimulator patterns followed correctly
- Finalizer coordination works with Composition Controller
- Multiple controller instances operate without conflicts

**Functional Verification**:

- Deploy nginx chart from Bitnami repository with values override
- Component progresses through correct phase transitions
- Release cleanup completes during Component deletion
- Error scenarios produce actionable failure information

**Technical Validation**:

- JSON unmarshaling from Component.Spec.Config works correctly
- Configuration supports nginx chart deployment from Bitnami repository
- Install nginx chart from Bitnami repository (charts.bitnami.com/bitnami)
- Query installed release status successfully
- Status fields accurately reflect controller operations
- Timestamps recorded for key lifecycle events
- Project compiles without import errors
- No version conflicts in dependency resolution

**Test Coverage**:

- All protocol compliance tests pass
- Helm operations tested end-to-end
- Error scenarios covered with appropriate responses
- Tests integrate with existing CI/CD pipeline

**Integration Requirements**:

- Follow existing handler patterns in internal/controller/rds for structure
- Use same RBAC and manager setup patterns from cmd/main.go
