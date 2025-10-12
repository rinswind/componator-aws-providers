# Implementation Plan: Plugin Architecture for Helm Chart Sources

## Feature Overview

Refactor helm chart sources (HTTP and OCI) into self-contained, independently testable plugins that implement a common interface. This eliminates type-switching forks in the helm controller, improves testability, and enables easy addition of new source types (git, S3, local) without modifying core controller logic.

## Architecture Impact

**Patterns Involved:**

- Plugin/Registry pattern for source type discovery and instantiation
- Factory pattern for source creation with dependencies
- Interface-based polymorphism to eliminate type switching

**Components Affected:**

- `internal/controller/helm/source/` - Chart source implementations (HTTP, OCI)
- `internal/controller/helm/config.go` - Configuration parsing and source creation
- `internal/controller/helm/source_config.go` - Source configuration models
- `internal/controller/helm/operations.go` - Operations factory and chart source usage

**Integration Points:**

- Helm operations factory creates sources via registry
- Sources parse and validate their own configuration
- Sources are self-contained with no shared state beyond interface

**Key Constraints:**

- Must maintain backward compatibility with existing Component specs
- HTTP caching repository singleton must remain shared across reconciliations
- OCI authentication requires Kubernetes client access
- Interface must support both sync (HTTP) and async (future) chart retrieval

**Trade-offs:**

- More initial refactoring complexity vs. easier future extensibility
- Stronger interface contracts vs. flexibility in implementation
- Package organization complexity vs. clear separation of concerns

## API Changes

**New Package:** `internal/controller/helm/sources/`

**New Types:**

- `ChartSource`: Interface that all source types implement
  - `Type() string` - Returns source type identifier
  - `ParseAndValidate(ctx, rawConfig) error` - Parse and validate source-specific configuration
  - `GetChart(ctx, settings) (*chart.Chart, error)` - Fetch chart from source
  - `GetVersion() string` - Return configured chart version

- `Registry`: Source instance registry (simple lookup table)
  - `Register(sourceType string, source ChartSource)` - Register a source instance
  - `Get(sourceType string) (ChartSource, error)` - Retrieve registered source instance

**Modified Types:**

- `HelmConfig`: Remove `Source SourceConfig` field, simplify to core helm config only
- `HelmOperationsFactory`: Add `sourceRegistry *Registry` field
- `HelmOperations`: Keep `chartSource source.ChartSource` but now references new interface

**Removed Types:**

- `SourceConfig` interface (replaced by `ChartSource`)
- `HTTPSource` struct (moved to `sources/http` package)
- `OCISource` struct (moved to `sources/oci` package)
- Helper methods `GetHTTPSource()` and `GetOCISource()` (eliminated by polymorphism)

**New Functions:**

- `sources.NewRegistry() *Registry` - Create source registry
- `sources.DetectSourceType(rawConfig) (string, error)` - Extract type field from config
- `sources/http.NewSource(httpRepo *http.CachingRepository) ChartSource` - Create HTTP source instance
- `sources/oci.NewSource(k8sClient client.Client) ChartSource` - Create OCI source instance

## Critical Logic and Decisions

### Component: Source Registry

**Responsibilities:**

- Maintain map of source type → source instance
- Provide lookup for registered sources
- Validate source type is registered

**Critical flow:**

```text
Registry.Register(sourceType, source):
  store source instance in map[sourceType]

Registry.Get(sourceType):
  source = lookup(sourceType)
  if source not found:
    return error "unknown source type"
  return source
```

**Design decisions:**

- Decision: Store source instances, not factory functions - Sources are long-lived singletons created once in NewComponentReconciler
- Decision: Registry is simple lookup table - With only 2-3 known source types, complex factory patterns are unnecessary
- Decision: Sources created explicitly in NewComponentReconciler - Clear dependency management, no hidden initialization

### Component: HTTP Source Package

**Responsibilities:**

- Parse and validate HTTP repository configuration
- Wrap singleton CachingRepository for chart retrieval
- Implement ChartSource interface

**Critical flow:**

```text
HTTPSource.ParseAndValidate(rawConfig):
  unmarshal JSON into HTTPConfig struct
  validate using validator framework:
    - repository URL format
    - chart name and version present
  
  store validated config internally
  return error if validation fails

HTTPSource.GetChart(ctx, settings):
  use stored config (repoName, repoURL, chartName, version)
  delegate to singleton CachingRepository
  return chart or error
```

**Design decisions:**

- Decision: Keep CachingRepository as singleton - **Critical for performance**: Large repos like Bitnami use ~40MB memory + 100MB disk per cache. Per-source caches would multiply this cost unacceptably.
- Decision: HTTP source is long-lived singleton - Created once in NewComponentReconciler, stores httpRepo reference
- Decision: Config parsed per reconciliation - ParseAndValidate called each time with fresh config from Component spec

### Component: OCI Source Package

**Responsibilities:**

- Parse and validate OCI registry configuration
- Handle authentication via Kubernetes secrets
- Pull charts from OCI registries using Helm SDK

**Critical flow:**

```text
OCISource.ParseAndValidate(rawConfig):
  unmarshal JSON into OCIConfig struct
  validate:
    - chart reference format (oci://...)
    - authentication config if present
  
  store validated config and dependencies
  return error if validation fails

OCISource.GetChart(ctx, settings):
  if authentication configured:
    resolve credentials from K8s secret
    authenticate to registry
  
  pull chart using Helm action.Pull
  load chart from downloaded archive
  return chart or error
```

**Design decisions:**

- Decision: OCI source is long-lived singleton - Created once in NewComponentReconciler with k8sClient reference
- Decision: Parse full OCI reference internally - Keep parsing logic within OCI source
- Decision: No fallback authentication - Fail fast if configured secret not found

### Component: Helm Operations Factory

**Responsibilities:**

- Detect source type from Component config
- Retrieve registered source from registry
- Parse source-agnostic helm configuration
- Assemble HelmOperations with configured source

**Critical flow:**

```text
NewOperations(rawConfig, currentStatus):
  parse minimal HelmConfig (releaseName, namespace, values)
  parse status
  
  sourceType = DetectSourceType(rawConfig)
  chartSource = registry.Get(sourceType)
  
  chartSource.ParseAndValidate(ctx, rawConfig)
  
  return HelmOperations{
    config: helmConfig,
    status: helmStatus,
    chartSource: chartSource,
    actionConfig: actionConfig
  }
```

**Design decisions:**

- Decision: Registry stores pre-created sources - Sources are singletons, retrieved and configured per reconciliation
- Decision: Two-stage usage (retrieve then configure) - Necessary for polymorphic config with reusable source instances
- Decision: Source parses its own config section - Each source owns its config schema
- Decision: Fail fast on unknown source type - Clear error messages for configuration issues

### Component: Source Lifecycle and Configuration

**Source Creation (once per controller):**

```text
NewComponentReconciler:
  httpRepo = NewCachingRepository(...)
  
  httpSource = sources.NewHTTPSource(httpRepo)
  ociSource = sources.NewOCISource(k8sClient)
  
  registry = sources.NewRegistry()
  registry.Register("http", httpSource)
  registry.Register("oci", ociSource)
  
  factory = NewHelmOperationsFactory(registry)
```

**Source Usage (per reconciliation):**

```text
NewOperations:
  sourceType = DetectSourceType(rawConfig)
  source = registry.Get(sourceType)
  source.ParseAndValidate(ctx, rawConfig)
  
  use configured source for chart operations
```

**Design decisions:**

- Decision: Sources are long-lived, config is per-reconciliation - Sources created once, configured many times
- Decision: Maintain JSON schema compatibility - No breaking changes to Component CRDs
- Decision: Sources parse their entire "source" section - Clean ownership of config schema
- Decision: Helm controller only parses common fields - releaseName, namespace, values, manageNamespace

## Testing Approach

**Unit Tests:**

- `sources/` package: Registry registration and source creation
- `sources/http/` package: HTTP source config parsing, validation, chart retrieval
- `sources/oci/` package: OCI source config parsing, authentication, chart pulling
- Each source tested independently without helm controller

**Integration Tests:**

- `internal/controller/helm/` package: Operations factory with source registry
- End-to-end: Component creation → config parsing → chart retrieval → deployment
- Test both HTTP and OCI sources through full reconciliation loop

**Critical Scenarios:**

- Valid HTTP source configuration with various repositories
- Valid OCI source configuration with and without authentication
- Invalid source type detection and error handling
- Missing required configuration fields
- Source switching between reconciliations (config updates)
- Registry errors and credential resolution failures

## Implementation Phases

### Phase 1: Create Source Infrastructure

- Create `sources/` package with `ChartSource` interface and `Registry`
- Implement `DetectSourceType` function for config type detection
- Registry stores source instances (not factory functions)
- **Deliverable:** Interface and registry compile and pass basic registration/lookup tests

### Phase 2: Migrate HTTP Source

- Create `sources/http/` package with HTTP source implementation
- Move HTTP configuration types from `source_config.go`
- Implement `ChartSource` interface wrapping existing `http.CachingRepository`
- Constructor `NewSource(httpRepo)` stores repo reference, no config yet
- `ParseAndValidate` parses and stores config from Component spec
- Update tests to use new package structure
- **Deliverable:** HTTP source works as singleton with per-reconciliation config

### Phase 3: Migrate OCI Source

- Create `sources/oci/` package with OCI source implementation
- Move OCI configuration types from `source_config.go`
- Implement `ChartSource` interface using existing OCI chart pulling logic
- Constructor `NewSource(k8sClient)` stores client reference
- `ParseAndValidate` parses and stores config from Component spec
- Update tests to use new package structure
- **Deliverable:** OCI source works as singleton with per-reconciliation config

### Phase 4: Update Helm Controller

- Update `NewComponentReconciler` to create source instances and populate registry
- Modify `HelmOperationsFactory` to accept and use source registry
- Update `NewOperations` to detect source type and retrieve from registry
- Simplify `HelmConfig` to remove polymorphic `Source` field
- Remove type-switching logic from `operations.go`
- Update all references to use new `ChartSource` interface
- **Deliverable:** Controller creates sources once, retrieves and configures per reconciliation

### Phase 5: Cleanup and Documentation

- Remove old `source_config.go` after migration
- Remove `SourceConfig` interface and helper methods (`GetHTTPSource`, `GetOCISource`)
- Update package documentation and examples
- Verify all integration tests pass
- Document source architecture and how to add new source types
- **Deliverable:** Clean architecture with comprehensive documentation

## Key Design Decisions

### HTTP Repository Caching

The HTTP `CachingRepository` will remain a singleton shared across all HTTP source instances. This is essential for performance: large repositories like Bitnami maintain ~40MB indexes in memory and 100MB YAML on disk. Creating per-source repositories would multiply this cost unacceptably.

**Trade-off:** Sources share index cache state, but memory/disk footprint remains manageable.

### Source Instance Management

Source instances will be created once in `NewComponentReconciler` and registered with the registry. Each source is a long-lived singleton that stores its dependencies (HTTP repo, k8sClient) but receives fresh configuration on each reconciliation via `ParseAndValidate`.

**Implementation:**

- HTTP source created with `httpRepo` reference
- OCI source created with `k8sClient` reference
- Both registered in registry for lookup by type
- No factory functions or dependency wrapper structs needed

### No Version Compatibility

Sources will not include version compatibility markers. All sources are compiled with the controller as part of the same binary. There will be no external plugin loading, making versioning unnecessary overhead.
