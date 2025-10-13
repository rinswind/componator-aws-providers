# Implementation Plan: OCI Registry Chart Source

## Feature Overview

Add OCI registry support to the Helm handler, enabling chart retrieval from OCI-compliant registries (GitHub Container Registry, Docker Hub, etc.) alongside HTTP repository support through a unified source configuration interface. This provides chart addressing through `oci://` URLs with embedded registry, chart name, and version, plus secure registry authentication through Kubernetes secrets. As an alpha release, this introduces a breaking change to configuration schema that requires updating existing configurations.

## Architecture Impact

**Affected Components:**

- Chart source abstraction layer (new OCI implementation)
- Configuration parsing (two-stage type detection)
- Credential resolution (new registry authentication)
- HelmOperations factory (source type routing)

**Integration Points:**

- ChartSource interface: Add OCIChartSource implementation
- HelmConfig: Add Source field with type-based polymorphism
- HelmOperationsFactory: Route to correct source based on configuration

**Key Constraints:**

- Follow existing security patterns (namespace scoping, secret resolution)
- Leverage Helm's native OCI support (no custom registry client)
- No changes to Component CRD or protocol compliance
- Breaking change acceptable for alpha release (existing configs must be updated)

**Trade-offs:**

- Two-stage parsing adds complexity but enables clean type-specific schemas
- Polymorphic source configuration vs. separate config types (chose polymorphism for unified interface)
- Helm SDK OCI support vs. custom implementation (chose SDK for maintenance and standards compliance)
- Breaking change vs. backward compatibility (chose clean break for simpler long-term maintainability)

## API Changes

### New Types

**ChartSource Interface** (simplified):

```go
type ChartSource interface {
    GetChart(ctx context.Context, settings *cli.EnvSettings) (*chart.Chart, error)
}
```

**HTTPChartSource**: Wraps shared HTTP singleton with per-chart config

```go
type HTTPChartSource struct {
    client    *http.ChartSource  // Shared singleton
    repoName  string
    repoURL   string
    chartName string
    version   string
}
```

**OCIChartSource**: Stateful OCI chart source

```go
type OCIChartSource struct {
    chartRef string                // "oci://registry/path:version"
    auth     *OCIAuthentication
}
```

**OCISource**: OCI-specific configuration structure

```go
type OCISource struct {
    Type           string             `json:"type" validate:"eq=oci"`
    Chart          string             `json:"chart" validate:"required,oci_reference"`
    Authentication *OCIAuthentication `json:"authentication,omitempty"`
}
```

**OCIAuthentication**: Registry credential configuration

```go
type OCIAuthentication struct {
    Method    string    `json:"method" validate:"eq=registry"`
    SecretRef SecretRef `json:"secretRef" validate:"required"`
}
```

**SecretRef**: Common credential reference (shared across source types)

```go
type SecretRef struct {
    Name      string `json:"name" validate:"required"`
    Namespace string `json:"namespace" validate:"required"`
}
```

**SourceConfig**: Interface for polymorphic source configuration

```go
type SourceConfig interface {
    GetType() string
    GetAuthentication() interface{}
}
```

### Modified Types

**HelmConfig**: Replace repository/chart fields with polymorphic source (breaking change)

- Remove: `Repository HelmRepository`, `Chart HelmChart` fields
- Add: `Source SourceConfig` (required field)
- Keep: All other fields (ReleaseName, ReleaseNamespace, Values, etc.) unchanged

**HTTPSource**: Wrap existing repository/chart as HTTP source type

```go
type HTTPSource struct {
    Type       string         `json:"type" validate:"eq=http"`
    Repository HelmRepository `json:"repository" validate:"required"`
    Chart      HelmChart      `json:"chart" validate:"required"`
}
```

### New Functions

**resolveSourceConfig(rawSource json.RawMessage) (SourceConfig, error)**: Two-stage parsing with type detection

**resolveOCICredentials(ctx, secretRef, namespace) (username, password, token string, error)**: Secret resolution with namespace scoping

**HTTPChartSource.GetChart(ctx, settings) (*chart.Chart, error)**: Delegates to shared singleton with stored parameters

**OCIChartSource.GetChart(ctx, settings) (*chart.Chart, error)**: Parses reference, authenticates, pulls chart

## Critical Logic and Decisions

### Component: ChartSource Interface Simplification

**Key Design Decision**: Simplify the ChartSource interface by moving addressing parameters from method call to construction time. This eliminates API friction between HTTP and OCI addressing models.

**New Interface**:
```go
type ChartSource interface {
    GetChart(ctx context.Context, settings *cli.EnvSettings) (*chart.Chart, error)
}
```

**Architecture Pattern**:
- Factory parses source configuration and creates fully-configured ChartSource instances
- HTTPChartSource wraps shared singleton with per-chart configuration
- OCIChartSource constructed with chart reference and authentication
- HelmOperations remains completely source-agnostic

**Benefits**: Clean interface, no parameter mapping, each source uses natural addressing, easy extensibility.

### Component: Configuration Parsing

**Key Responsibilities:**

- Detect source type from configuration
- Parse type-specific schemas
- Validate credentials and references
- Enforce required source field

**Critical Flow:**

```text
resolveHelmConfig:
  parse common fields (releaseName, namespace, values)
  
  validate "source" field exists (required)
  call resolveSourceConfig(rawSource)
  return parsed config
    
resolveSourceConfig:
  parse type field only
  
  if type == "http":
    parse HTTPSource schema
    validate repository URL and chart fields
  else if type == "oci":
    parse OCISource schema
    validate OCI reference format (oci://registry/path:version)
  else:
    return unsupported type error
```

**Design Decision: Two-stage parsing** - Parse type field first, then parse full schema. This enables clean validation and type-specific error messages without complex unmarshaling logic.

**Design Decision: Breaking change for clean architecture** - Require source field from day 1 rather than supporting legacy formats. Simpler implementation and clearer migration path for alpha users.

### Component: OCI Chart Resolution

**Key Responsibilities:**

- Authenticate to OCI registries
- Resolve credentials from secrets
- Use Helm SDK for OCI operations
- Cache credentials appropriately

**Critical Flow:**

```text
GetChart(ociChart, settings):
  parse OCI reference (oci://registry/repo/chart:version)
  
  if authentication configured:
    resolve credentials from secret (namespace scoping)
    configure registry client with credentials
  
  use Helm SDK registry.Login() for authentication
  use Helm SDK registry.Client.Pull() for chart retrieval
  load chart from pulled artifact
  
  return loaded chart

resolveOCICredentials:
  try Component namespace first
  if not found, try system namespace (deployment-system)
  
  if secret exists:
    extract username+password OR token
    return credentials
  else:
    return authentication error
```

**Design Decision: Helm SDK native OCI support** - Use `helm.sh/helm/v3/pkg/registry` rather than custom registry client. Ensures OCI standards compliance and reduces maintenance burden.

**Design Decision: Namespace-scoped credential resolution** - Follow existing security pattern (Component namespace first, system namespace fallback). Maintains isolation while allowing shared credentials.

### Component: Source Type Routing and Factory Pattern

**Key Responsibilities:**

- Route chart requests to correct source type
- Create configured ChartSource instances
- Manage shared HTTP singleton
- Inject source into HelmOperations

**Critical Flow:**

```text
HelmOperationsFactory.NewOperations:
  parse configuration (includes source)
  
  var chartSource source.ChartSource
  
  switch source type:
    case HTTPSource:
      create HTTPChartSource wrapping shared singleton:
        chartSource = &HTTPChartSource{
          client:    f.httpChartSource,  // shared singleton
          repoName:  src.Repository.Name,
          repoURL:   src.Repository.URL,
          chartName: src.Chart.Name,
          version:   src.Chart.Version,
        }
    
    case OCISource:
      create OCIChartSource with chart config:
        chartSource = &OCIChartSource{
          chartRef: src.Chart,           // "oci://registry/path:version"
          auth:     src.Authentication,
        }
  
  create HelmOperations with chartSource
  
HelmOperations.Deploy:
  chart, err := h.chartSource.GetChart(ctx, h.settings)
  // Source-agnostic! No conditional logic needed.
```

**Design Decision: Adapter pattern for HTTP singleton** - HTTPChartSource wraps the shared http.ChartSource singleton with per-chart configuration, enabling simple interface while preserving singleton benefits (caching, index management).

**Design Decision: Stateful OCI source** - OCIChartSource stores chart reference and authentication at construction, making GetChart() calls simple and source-agnostic.

### Component: Security and Validation

**Key Responsibilities:**

- Validate OCI references
- Enforce TLS for registry connections
- Prevent credential leakage
- Audit authentication attempts

**Validation Rules:**

- OCI reference format: `oci://registry.example.com/path/to/chart:version`
- Secret reference must specify both name and namespace
- Registry authentication method must be "registry"
- Component-namespace secrets take precedence

**Error Categories:**

- Configuration errors: Invalid OCI reference, missing required fields
- Authentication failures: Secret not found, invalid credentials, registry rejection
- Network errors: Connection failures, timeout, TLS errors
- Chart resolution errors: Chart not found, version mismatch

## Testing Approach

### Unit Tests

**Configuration Parsing:**

- Source type detection (http, oci)
- OCI reference validation
- HTTPSource configuration validation
- Error handling for invalid configurations and missing source field

**Credential Resolution:**

- Namespace scoping (Component ns, system ns, not found)
- Secret format handling (username/password, token)
- Error handling for missing/invalid secrets

**Source Routing:**

- Factory routes to correct source based on type
- Operations receive appropriate source implementation

### Integration Tests

**OCI Chart Operations:**

- Authenticate to public OCI registry
- Pull chart from authenticated private registry
- Handle authentication failures gracefully
- Test with GitHub Container Registry (common use case)

**End-to-End Scenarios:**

- Deploy Component with OCI source
- Verify protocol compliance (claiming, deployment, deletion)
- Test credential rotation (update secret, verify reauth)

### Critical Scenarios

- `parse_oci_config_with_auth` - Valid OCI configuration with authentication
- `parse_oci_config_without_auth` - Public registry without authentication
- `parse_http_config` - HTTP source configuration validation
- `parse_config_missing_source` - Error handling for missing source field
- `resolve_credentials_component_namespace` - Credential resolution priority
- `resolve_credentials_system_namespace` - Fallback credential resolution
- `oci_authentication_failure` - Handle invalid credentials
- `oci_chart_not_found` - Handle missing chart/version

## Implementation Phases

### Phase 1: Configuration Schema and Parsing ✅ COMPLETE

**Goals:**
- ✅ Define OCI and HTTP source types
- ✅ Implement two-stage parsing with type detection
- ✅ Simplified to flat config (no breaking change)
- ✅ Add validation for OCI references

**Deliverable:** Configuration parsing handles both HTTP and OCI sources. Backward compatibility maintained. Unit tests pass for all parsing scenarios.

**Git commits:**
- `d30d3f3` - feat: add OCI registry support for Helm charts (initial implementation with breaking change)
- `270a3b2` - refactor: implement helm source plugin architecture (simplified to flat config, no breaking change)

**Implementation Evolution:** The original plan proposed nested `source` config, but final implementation maintained flat structure for backward compatibility while internally using plugin architecture.

### Phase 2: OCI Source Implementation ✅ COMPLETE

**Goals:**
- ✅ Implement OCISource with Helm SDK registry client
- ✅ Add credential resolution with namespace scoping
- ✅ Handle OCI chart pulling and loading
- ✅ Integrate with factory pattern

**Deliverable:** OCI source can authenticate and pull charts from registries. Unit tests validate credential resolution and chart operations.

**Git commits:**
- `d30d3f3` - feat: add OCI registry support for Helm charts
- `18b828e` - feat: fix OCI source using ChartDownloader for proper authentication

**Key Files:**
- `sources/oci/source.go` - OCISource implementation with LocateChart
- `sources/oci/config.go` - OCI configuration types and validation
- `sources/oci/source_test.go` - OCI source tests

### Phase 3: Source Type Routing ✅ COMPLETE

**Goals:**
- ✅ Modify factory to route based on source type
- ✅ Update HelmOperations to use polymorphic source
- ✅ Maintain HTTP source functionality
- ✅ Add logging for source selection

**Deliverable:** Factory correctly routes to HTTP or OCI source. Existing HTTP deployments work unchanged. Logging shows source selection.

**Git commits:**
- `270a3b2` - refactor: implement helm source plugin architecture
- `38e8078` - refactor: implement composite pattern for helm chart source factories

**Key Architecture:**
- Composite Registry pattern for source type detection and delegation
- Factory pattern creates immutable per-reconciliation sources
- Type detection from config structure (internal, not exposed to users)

### Phase 4: Integration and Testing ⚠️ IN PROGRESS

**Goals:**
- Integration tests with real OCI registries
- End-to-end Component deployment with OCI charts
- Protocol compliance validation
- Error handling and recovery scenarios

**Current Status:**
- ✅ Unit tests pass for all source implementations
- ✅ HTTP and OCI sources validated separately
- ✅ Factory pattern and routing tested
- ⚠️ End-to-end integration tests with real OCI registries (pending)
- ⚠️ Protocol compliance validation with OCI sources (pending)

**Deliverable:** Integration tests pass with GitHub Container Registry. Components deploy successfully with OCI sources. All protocols remain compliant.

## Open Questions (Resolved)

**Q: Which OCI registries should be tested?**
✅ **Resolved**: GitHub Container Registry as primary target. Implementation uses Helm SDK's standard OCI support, compatible with all OCI-compliant registries.

**Q: Should we support custom CA certificates for private registries?**
✅ **Resolved**: Use system CA bundle. Custom CA support deferred - can be added later if needed.

**Q: How should we handle OCI artifact digests vs. tags?**
✅ **Resolved**: Support tags (versions) only via OCI reference format `oci://registry/path:version`. Digest support can be added in future if required.

**Q: Should credential caching be implemented for performance?**
✅ **Resolved**: Authenticate per-chart-pull. Factory pattern creates immutable sources per-reconciliation, eliminating shared state concerns. Performance optimization can be added later based on metrics.

## Implementation Status: MOSTLY COMPLETE

### Summary

✅ **Primary Goal Achieved**: OCI registry support fully implemented and working  
✅ **Phases 1-3 Complete**: Configuration parsing, OCI implementation, source routing all done  
⚠️ **Phase 4 In Progress**: Integration testing with real OCI registries pending

### Key Accomplishments

**Phase 1 (d30d3f3, 270a3b2):**
- OCI and HTTP source types defined
- Flat configuration maintained (no breaking change)
- Type detection from config structure
- OCI reference validation

**Phase 2 (d30d3f3, 18b828e):**
- OCISource implementation with ChartDownloader
- Credential resolution from Kubernetes secrets
- Authentication to OCI registries
- Chart pulling and loading

**Phase 3 (270a3b2, 38e8078):**
- Composite Registry pattern for type detection
- Factory pattern for source creation
- Source type routing transparent to users
- HTTP source functionality maintained

### Architecture Evolution

The implementation evolved significantly from the original plan:

1. **API Simplification**: Rejected breaking change with nested `source` config. Maintained flat structure for better UX.

2. **Interface Evolution**: Started with `GetChart(ctx, settings) (*chart.Chart, error)`, evolved to `LocateChart(ctx) (string, error)` for cleaner architecture (see refactor-helm-source-to-locatechart.md).

3. **Factory Pattern Integration**: Combined OCI support with factory pattern from helm-source-factory-pattern.md for thread-safe concurrent reconciliation.

4. **Plugin Architecture**: Implemented full plugin architecture from helm-source-plugin-architecture.md with registry pattern and interface-based polymorphism.

### Current State

```text
User Configuration (flat, backward compatible):
  repository + chart → HTTP source
  chart (OCI format) → OCI source

Internal Architecture:
  Composite Registry
  ├── HTTP Factory → HTTPSource (per-reconciliation)
  └── OCI Factory → OCISource (per-reconciliation)

Chart Operations:
  LocateChart() → string path → loader.Load() → *chart.Chart
```

### Verification

All unit tests pass:
```bash
go test ./internal/controller/helm/sources/... -v
# PASS: composite, http, oci sources
```

Integration with dependency validation and factory pattern complete.

## Migration Notes

**Breaking Change Decision Reversed:** The original plan proposed a breaking change with polymorphic `source` field. However, during implementation (commit 270a3b2), this was **simplified to maintain backward compatibility**.

**Final Implementation - No Breaking Change:**

The configuration schema remains **flat** without the nested `source` wrapper. The source type is detected internally from the presence of specific fields:

```yaml
# HTTP Source (detected by presence of repository field)
config:
  releaseName: my-app
  releaseNamespace: default
  repository:
    url: https://charts.bitnami.com/bitnami
    name: bitnami
  chart:
    name: postgresql
    version: "12.1.2"
```

```yaml
# OCI Source (would be detected by OCI reference format)
config:
  releaseName: my-app
  releaseNamespace: default
  chart: oci://registry.example.com/charts/app:1.0.0
  authentication:
    method: registry
    secretRef:
      name: registry-credentials
      namespace: default
```

**Design Evolution:** The factory pattern and plugin architecture were maintained for internal implementation, but the external API was simplified to avoid breaking changes for users.
