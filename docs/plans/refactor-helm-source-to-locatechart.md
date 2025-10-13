# Implementation Plan: Fix OCI Source and Align Helm Source Architecture

## Problem Statement

The OCI source implementation is broken - it uses the `action.Pull` CLI command wrapper which doesn't return the downloaded chart path, forcing unsafe path reconstruction. Attempts to quick-fix using `LocateChart` failed because OCI registry authentication (via `RegistryClient`) cannot be passed through `LocateChart` - only Install/Upgrade actions configure this.

**Root cause:** Current architecture has sources returning loaded `*chart.Chart` objects. This works for HTTP (which uses `LocateChart` internally) but breaks for OCI which needs direct `ChartDownloader` access for authentication.

**Solution:** Refactor both sources to use `ChartDownloader` directly, returning string paths. This fixes OCI authentication, creates symmetric implementations, and aligns with Helm's architectural patterns.

## Feature Overview

Refactor the Helm chart source subsystem to return string paths instead of loaded chart objects, using `ChartDownloader` directly for both HTTP and OCI sources. This is **mandatory to fix broken OCI source** while creating architectural consistency where sources use Helm's download primitives directly, and Deploy action handles chart loading, dependency validation, and installation orchestration.

## Architecture Impact

**Affected Components:**
- `ChartSource` interface: Change return type from `*chart.Chart` to `string` (path)
- HTTP Source: Use `ChartDownloader` directly instead of wrapping `LocateChart`
- OCI Source: Use `ChartDownloader` directly with registry client
- `HelmOperations.Deploy()`: Handles chart loading, dependency validation, and installation
- `CachingRepository`: Change `loadChartFromIndex` to use `ChartDownloader` and return path

**Key Integration Points:**
- Both sources use `downloader.ChartDownloader` directly (symmetric architecture)
- Deploy action takes responsibility for `loader.Load()`, `action.CheckDependencies()`, and installation
- Dependency validation enforces pre-packaged charts (fail fast on missing dependencies)
- No wrapping of `LocateChart` - sources use Helm's download primitives directly

**Constraints:**
- Charts must be pre-packaged with dependencies included
- No automatic dependency downloading (security and predictability)
- Maintains in-memory IndexFile caching for HTTP sources (performance optimization)

**Why This Approach:**

**Alternatives Tried and Failed:**
1. **Quick-fix: Use Pull action** - Pull returns void, requires reconstructing cache path unsafely
2. **Quick-fix: Use LocateChart for OCI** - Cannot pass RegistryClient for authentication through LocateChart
3. **Conclusion:** ChartDownloader is the only Helm primitive that:
   - Takes RegistryClient directly (solves OCI auth)
   - Returns explicit path (no reconstruction hacks)
   - Works identically for HTTP and OCI (solves symmetry)

**Trade-offs:**
- ✅ **Fixes broken OCI source** - Primary motivation, mandatory work
- ✅ Symmetric architecture: both sources use ChartDownloader directly
- ✅ Clean type flow: string → chart → use (no conversions)
- ✅ Clear separation: sources locate, deploy orchestrates
- ✅ Less indirection: using download primitives, not wrapping LocateChart
- ❌ Breaking change to ChartSource interface (acceptable: alpha release)
- ❌ No automatic chart verification (provenance files) - can add later if needed
- ✅ Enforces dependency pre-packaging (safer for operators)

## API Changes

**Modified Interface:**

```go
// ChartSource - change GetChart return type
type ChartSource interface {
    Type() string
    ParseAndValidate(ctx context.Context, rawConfig json.RawMessage) error
    LocateChart(ctx context.Context, settings *cli.EnvSettings) (string, error)  // Changed: was GetChart() (*chart.Chart, error)
    GetVersion() string
}
```

**No New Types** - all existing types remain unchanged.

**Modified Functions:**

- `HTTPSource.GetChart()` → `HTTPSource.LocateChart()`: Returns string path instead of loaded chart
- `OCISource.GetChart()` → `OCISource.LocateChart()`: Returns string path using ChartDownloader
- `HelmOperations.getChart()`: Replaced by inline LocateChart + loader.Load in Deploy
- `HelmOperations.Deploy()`: Extended to handle chart loading and dependency validation

## Critical Logic and Decisions

### Component: HTTP Source

**Responsibilities:**
- Parse HTTP repository configuration
- Use cached IndexFile to find chart metadata
- Use ChartDownloader to download chart to Helm cache
- Return path to cached tarball

**Critical flow:**
```text
LocateChart(ctx, settings):
  if config is nil:
    return error "ParseAndValidate not called"
  
  delegate to CachingRepository.loadChartFromIndex():
    - check in-memory IndexFile cache
    - find chart version in index
    - create ChartDownloader with settings
    - call dl.DownloadToCache(chartURL, version)
    - return path to downloaded .tgz file
  
  return path string
```

**Design decisions:**
- Decision: Use ChartDownloader directly - Required for symmetric implementation with OCI, simpler than wrapping LocateChart
- Decision: Keep IndexFile caching - Performance optimization for large repos (Bitnami 40MB index)
- Decision: No chart verification - Most charts lack provenance files, can add later if needed
- Decision: Return path from DownloadToCache - Explicit path return, no reconstruction needed

### Component: OCI Source

**Responsibilities:**
- Parse OCI chart reference
- Resolve authentication credentials from Kubernetes secrets
- Download chart from OCI registry to Helm cache using ChartDownloader
- Return path to cached tarball

**Critical flow:**
```text
LocateChart(ctx, settings):
  if config is nil:
    return error "ParseAndValidate not called"
  
  if authentication configured:
    resolve credentials from k8s secret
    create registry client with auth
  
  create ChartDownloader:
    - configure with settings.RepositoryCache
    - set RegistryClient if authenticated
    - add getter.WithRegistryClient option for OCI
  
  call dl.DownloadToCache(chartRef, "")
  return path to cached chart
```

**Design decisions:**
- Decision: Use ChartDownloader directly - **Only Helm API that accepts RegistryClient for OCI auth**
- Decision: Replace Pull action approach - Pull returns void, forces unsafe path reconstruction
- Decision: Pass RegistryClient to ChartDownloader - Direct authentication control
- Decision: Use DownloadToCache - Returns explicit path, same as HTTP source pattern

### Component: Deploy Action

**Responsibilities:**
- Locate chart using source
- Load chart from path
- Validate dependencies are pre-packaged
- Execute install or upgrade
- Handle errors appropriately

**Critical flow:**
```text
Deploy(ctx):
  chartPath = chartSource.LocateChart(ctx, settings)
  if error:
    return error "failed to locate chart"
  
  chart = loader.Load(chartPath)
  if error:
    return error "failed to load chart"
  
  if chart has dependencies:
    err = action.CheckDependencies(chart, chart.Metadata.Dependencies)
    if error:
      return error "chart has unfulfilled dependencies, must be pre-packaged"
  
  if release exists:
    return upgrade(ctx, chart)
  else:
    return install(ctx, chart)
```

**Design decisions:**
- Decision: Fail fast on missing dependencies - Users must pre-package charts with dependencies
- Decision: No automatic dependency download - Maintains predictability and security
- Decision: CheckDependencies is validation only - No `downloader.Manager.Update()` call

**Error handling:**
- Chart location failures → return I/O error
- Chart loading failures → return I/O error  
- Dependency validation failures → return user-facing error with guidance
- Install/upgrade failures → existing error handling unchanged

## Testing Approach

**Unit Tests:**
- HTTP Source: LocateChart returns valid path string
- OCI Source: LocateChart returns valid path string
- Both sources: ParseAndValidate must be called before LocateChart
- Deploy action: Handles chart loading and dependency validation

**Integration Tests:**
- HTTP chart deployment: Locate → Load → Install flow
- OCI chart deployment: Locate with auth → Load → Install flow
- Dependency validation: Chart with pre-packaged dependencies succeeds
- Dependency validation: Chart with missing dependencies fails fast with clear error

**Critical Scenarios:**
- Chart with embedded dependencies (validates successfully)
- Chart without dependencies (skips validation)
- Chart with missing dependencies (fails with user-friendly message)
- Multiple reconciliations (IndexFile cache hit on subsequent calls)

## Implementation Phases

### Phase 1: Update ChartSource Interface and HTTP Source ✅ COMPLETE

**Goals:**
- ✅ Rename `GetChart()` to `LocateChart()` in interface and implementations
- ✅ Change return type from `(*chart.Chart, error)` to `(string, error)`
- ✅ Update `CachingRepository.loadChartFromIndex()` to use ChartDownloader directly
- ✅ Remove `loader.Load()` call from `loadChartFromIndex()`, return path only
- ✅ Update tests for HTTP source
- ✅ Update OCI source for interface compliance
- ✅ Update Deploy action to handle chart loading
- ✅ Remove helper methods and update all tests

**Deliverable:** ✅ HTTP source uses ChartDownloader and returns chart path string; tests validate path return and caching behavior. Committed in b7ed719.

### Phase 2: Fix OCI Source (PRIMARY GOAL)

**Goals:**
- **Fix broken OCI implementation** by replacing Pull action with ChartDownloader
- Implement OCI `LocateChart()` using `downloader.ChartDownloader` directly
- Configure ChartDownloader with RegistryClient for authentication
- Pass RegistryClient through ChartDownloader options for OCI protocol
- Return explicit path from DownloadToCache (no reconstruction)
- Update tests for OCI source with authentication scenarios

**Deliverable:** **OCI source works with authentication**; uses ChartDownloader (symmetric with HTTP); tests validate authenticated OCI registry access and path return.

### Phase 3: Update Deploy Action

**Goals:**
- Replace `getChart()` helper with inline `LocateChart()` + `loader.Load()`
- Add `action.CheckDependencies()` validation before install/upgrade
- Implement fail-fast error handling for missing dependencies
- Remove unused `getChart()` and `getChartVersion()` helpers

**Deliverable:** Deploy action orchestrates full flow; dependency validation works; clear error messages.

### Phase 4: Integration Testing and Validation

**Goals:**
- Test complete flow with real charts
- Validate pre-packaged dependencies work
- Validate missing dependencies fail with clear guidance
- Verify IndexFile caching performance benefit

**Deliverable:** Integration tests pass; documentation updated; refactoring complete.

## Context: Why This Refactoring

**Failed Quick-Fix Attempts:**

1. **Attempt: Keep using Pull action, reconstruct path**
   - Problem: `action.Pull` returns void
   - Would need to reverse-engineer Helm's cache path logic
   - Fragile - breaks if Helm changes cache structure
   - Verdict: Unsafe hack

2. **Attempt: Use LocateChart for OCI like HTTP does**
   - Problem: Cannot pass `RegistryClient` for authentication
   - `LocateChart` expects Install/Upgrade to configure registry client
   - Can't use LocateChart standalone for authenticated OCI access
   - Verdict: Doesn't solve authentication problem

3. **Discovery: ChartDownloader is the right primitive**
   - Takes `RegistryClient` directly via options
   - Returns explicit path (no reconstruction)
   - Same API for HTTP and OCI (just different configuration)
   - This is what Install/Upgrade use internally anyway

**Conclusion:** The refactoring isn't optional architectural cleanup - it's the **only clean solution** that fixes OCI authentication while creating symmetric, maintainable source implementations.

## Open Questions

**Q: Should we provide a configuration flag to enable automatic dependency downloading?**
Current decision: No - fail fast on missing dependencies. Can be revisited if strong user demand emerges.

**Q: How should we handle Chart.Lock file validation?**
Current scope: CheckDependencies only - no lock file validation. Lock files are for development-time dependency resolution.

**Q: Should LocateChart extract tarball to temp directory for Manager compatibility?**
Current decision: No - return .tgz path. If dependency downloading is added later, that code would handle extraction.
