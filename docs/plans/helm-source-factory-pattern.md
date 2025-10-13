# Implementation Plan: Helm Chart Source Factory Pattern

## Feature Overview

Refactor the Helm chart source plugin architecture to eliminate race conditions caused by shared mutable state. Replace singleton ChartSource instances with a factory pattern where factories are stateless singletons that create per-reconciliation ChartSource instances with immutable configuration. This ensures thread-safety when multiple Component reconciliations occur concurrently.

## Architecture Impact

**Problem:** Current design uses singleton ChartSource instances that store mutable `config` fields. The `ParseAndValidate()` method mutates this shared state, creating race conditions when reconciliations run concurrently.

**Solution:** Factory pattern with two-tier architecture:
- **Tier 1: ChartSourceFactory** - Stateless singletons stored in registry
- **Tier 2: ChartSource** - Immutable per-reconciliation instances created by factories

**Affected Components:**
- `sources/chart_source.go` - Interface split into Factory and Source
- `sources/http/source.go` - Implements factory pattern
- `sources/oci/source.go` - Implements factory pattern  
- `sources/registry.go` - Stores factories instead of sources
- `helm/operations.go` - Uses factories to create sources
- `helm/controller.go` - Initializes factories instead of sources

**Key Constraint:** Must maintain backward compatibility with existing Component specs and behavior. Only internal implementation changes.

**Trade-offs:**
- **Pro:** Thread-safe by design, no locks needed
- **Pro:** Simpler lifecycle (no ParseAndValidate step)
- **Pro:** Better encapsulation (settings baked into source)
- **Con:** Minor memory overhead (one source instance per reconciliation)
- **Con:** All-or-nothing refactor (can't be done incrementally per source type)

## API Changes

### New Interfaces

**ChartSourceFactory:**
```go
type ChartSourceFactory interface {
    Type() string
    CreateSource(ctx context.Context, rawConfig json.RawMessage, settings *cli.EnvSettings) (ChartSource, error)
}
```

**ChartSource (modified):**
```go
type ChartSource interface {
    LocateChart(ctx context.Context) (string, error)  // Removed settings parameter
    GetVersion() string
}
```

### New Types

**http.Factory:**
- Purpose: Creates HTTP chart source instances
- Fields: `httpRepo *CachingRepository`

**http.HTTPSource (renamed from Source):**
- Purpose: Per-reconciliation HTTP chart source instance
- Fields: `httpRepo *CachingRepository`, `config Config`, `settings *cli.EnvSettings`

**oci.Factory:**
- Purpose: Creates OCI chart source instances
- Fields: `k8sClient client.Client`, `repositoryCache string`

**oci.OCISource (renamed from Source):**
- Purpose: Per-reconciliation OCI chart source instance
- Fields: `k8sClient client.Client`, `repositoryCache string`, `config Config`, `settings *cli.EnvSettings`

### Modified Types

**Registry:**
- Change: Store `ChartSourceFactory` instead of `ChartSource`
- Methods: `Register(factory ChartSourceFactory)`, `Get(sourceType string) (ChartSourceFactory, error)`

**HelmOperations:**
- Removed field: `settings *cli.EnvSettings` (no longer needed, baked into source)
- Changed field: `chartSource sources.ChartSource` (now per-reconciliation instance)

**HelmOperationsFactory:**
- Renamed field: `sourceRegistry` → `factoryRegistry`

### Removed Methods

- `ChartSource.ParseAndValidate()` - Logic moved into factory `CreateSource()`

## Critical Logic and Decisions

### Component: HTTP Factory

**Responsibilities:**
- Parse and validate HTTP source configuration
- Create immutable HTTPSource instances

**Critical flow:**
```text
CreateSource(ctx, rawConfig, settings):
  extract "source" section from rawConfig
  unmarshal into Config struct
  validate Config using validator
  if validation fails:
    return error with details
  return new HTTPSource{
    httpRepo: factory.httpRepo (shared singleton)
    config: parsed Config (immutable)
    settings: provided settings (immutable)
  }
```

**Design decisions:**
- Decision: Share CachingRepository across all HTTPSource instances
- Rationale: Repository has internal locking and caching, safe to share
- Decision: Store config and settings by value or pointer-to-immutable
- Rationale: Prevents accidental mutation after creation

### Component: OCI Factory

**Responsibilities:**
- Parse and validate OCI source configuration with OCI reference validation
- Create immutable OCISource instances with credential resolution capability

**Critical flow:**
```text
CreateSource(ctx, rawConfig, settings):
  extract "source" section from rawConfig
  unmarshal into Config struct
  validate Config including OCI reference format
  if validation fails:
    return error with details
  return new OCISource{
    k8sClient: factory.k8sClient (shared)
    repositoryCache: factory.repositoryCache (shared path)
    config: parsed Config (immutable)
    settings: provided settings (immutable)
  }
```

**Design decisions:**
- Decision: Share k8sClient across OCISource instances
- Rationale: Client is thread-safe, used only for secret reads
- Decision: Credential resolution remains lazy (in LocateChart)
- Rationale: Secrets may not exist at CreateSource time, authentication only needed for actual chart pull

### Component: Operations Factory Usage

**Responsibilities:**
- Initialize settings once per reconciliation
- Create chart source instance using factory
- Pass configured source to HelmOperations

**Critical flow:**
```text
NewOperations(ctx, rawConfig, currentStatus):
  initialize settings = cli.New()
  detect source type from rawConfig
  get factory from registry by source type
  create source = factory.CreateSource(ctx, rawConfig, settings)
  
  parse helm config (releaseName, namespace, etc.)
  initialize actionConfig using settings
  
  return HelmOperations{
    actionConfig: initialized config
    config: helm config
    status: parsed status
    chartSource: created source instance (per-reconciliation)
  }
```

**Design decisions:**
- Decision: Settings initialized before factory call
- Rationale: Settings needed for both source creation and actionConfig initialization
- Decision: Remove settings from HelmOperations struct
- Rationale: Settings already baked into chartSource, no longer needed
- Decision: chartSource is per-reconciliation, not shared
- Rationale: Eliminates race conditions, each reconciliation gets independent state

### Component: Controller Initialization

**Responsibilities:**
- Create shared singleton resources (CachingRepository)
- Initialize factories with shared resources
- Register factories in registry

**Critical flow:**
```text
NewComponentReconciler(k8sClient):
  create httpRepo = NewCachingRepository(...) (shared singleton)
  
  create httpFactory = http.NewFactory(httpRepo)
  create ociFactory = oci.NewFactory(k8sClient, repositoryCache)
  
  create registry = NewRegistry()
  register httpFactory in registry
  register ociFactory in registry
  
  create operationsFactory with factory registry
  
  return ComponentReconciler
```

**Design decisions:**
- Decision: CachingRepository remains singleton
- Rationale: Already thread-safe with internal locking, caching benefits from being shared
- Decision: Factories created once at controller startup
- Rationale: Stateless, no per-reconciliation overhead

## Testing Approach

**Unit Tests:**
- HTTP Factory: CreateSource with valid/invalid configs, concurrent creation
- OCI Factory: CreateSource with valid/invalid configs, credential scenarios
- HTTP Source: LocateChart without settings parameter, GetVersion
- OCI Source: LocateChart without settings parameter, authentication flow
- Registry: Factory registration and retrieval

**Integration Tests:**
- Concurrent reconciliation: Multiple Components reconcile simultaneously without race conditions
- HTTP source end-to-end: Create source, locate chart, deploy
- OCI source end-to-end: Create source with credentials, locate chart, deploy
- Mixed source types: HTTP and OCI components reconciling concurrently

**Race Detection:**
- Run all tests with `-race` flag
- Verify no data races in concurrent scenarios
- Critical: Test multiple reconciliations of same handler type (HTTP or OCI) concurrently

**Critical Scenarios:**
- Concurrent HTTP source creation with different configs
- Concurrent OCI source creation with different authentication
- Source creation failure handling
- Settings propagation through factory to source

## Implementation Phases

### Phase 1: Interface Refactoring
- Split `ChartSource` interface into `ChartSourceFactory` and `ChartSource`
- Update interface documentation with new architecture patterns
- Keep old interface temporarily for comparison during development

**Deliverable:** New interfaces compile, no implementation yet

### Phase 2: HTTP Source Factory Implementation
- Create `http/factory.go` with Factory struct and CreateSource
- Refactor `http/source.go` to remove ParseAndValidate, store settings
- Update LocateChart to use stored settings instead of parameter
- Update HTTP source tests to use factory pattern
- Test concurrent source creation

**Deliverable:** HTTP sources work through factory pattern, tests pass with `-race`

### Phase 3: OCI Source Factory Implementation
- Create `oci/factory.go` with Factory struct and CreateSource
- Refactor `oci/source.go` to remove ParseAndValidate, store settings
- Update LocateChart to use stored settings instead of parameter
- Update OCI source tests to use factory pattern
- Test credential resolution through factory

**Deliverable:** OCI sources work through factory pattern, tests pass with `-race`

### Phase 4: Registry and Controller Integration
- Update `registry.go` to store factories instead of sources
- Update `operations.go` to use factory pattern (initialize settings early, call CreateSource)
- Remove settings field from HelmOperations struct
- Update `controller.go` to initialize factories instead of sources
- Update all integration tests

**Deliverable:** Full controller works with factory pattern, all tests pass, no race conditions

### Phase 5: Cleanup and Verification
- Remove old interface definitions if any
- Run full test suite with `-race` flag
- Verify concurrent reconciliation scenarios
- Update package documentation
- Review for any remaining shared mutable state

**Deliverable:** Clean codebase, comprehensive test coverage, verified thread-safety

## Open Questions

None - the approach is well-defined based on analysis of existing race condition. The factory pattern is a standard solution for this type of concurrency problem.

## Success Criteria

- ✅ All tests pass with `-race` flag showing no data races
- ✅ ChartSource instances are immutable after creation
- ✅ Multiple concurrent reconciliations work correctly
- ✅ HTTP and OCI sources both implement factory pattern
- ✅ No shared mutable state between reconciliations
- ✅ Backward compatible with existing Component specs
