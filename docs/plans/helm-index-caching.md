# Helm Repository Index Caching Implementation Plan

## Problem Statement

The Helm handler allocates ~3.5GB during each chart deployment due to parsing the repository index file (25MB YAML for bitnami). For N deployments, this results in:

- **N × 3.5GB cumulative allocations**
- **N × 20 GC cycles**
- **25% CPU overhead** during parsing
- **1-2 seconds parsing time** per deployment

The parsed index is only **40MB in memory** and rarely changes, making it ideal for caching.

## Solution

Implement in-memory LRU cache for parsed `*repo.IndexFile` objects, following FluxCD's proven pattern.

## Architecture Overview

**Key Design Principle**: HTTPChartSource encapsulates ALL HTTP repository concerns. External code only calls `GetChart()`.

**Object Lifecycle**:

```go
// Singleton - created once at startup
HelmOperationsFactory {
    httpChartSource *HTTPChartSource  // Owns the singleton
}

// Per-reconciliation - references singleton
HelmOperations {
    chartSource *HTTPChartSource  // Reference to factory's singleton
}
```

**Data Flow**:

```go
// User calls (in operations_deploy.go):
chart, err := h.chartSource.GetChart(repoName, repoURL, chartName, version, settings)

// HTTPChartSource.GetChart() orchestrates:
GetChart() {
    ensureRepository(repoName, repoURL)           // Step 1: Update repositories.yaml (flock)
    
    if index := indexCache.Get(repoName); found { // Step 2: Check in-memory cache
        return loadChartFromIndex(index, ...)     //   -> Cache hit path
    }
    
    index := loadOrDownloadIndex(repoName, ...)   // Step 3: Disk cache or network
    indexCache.Set(repoName, index)               // Step 4: Cache for next time
    return loadChartFromIndex(index, ...)         // Step 5: Load chart
}
```

**Cache Hierarchy**:

1. **In-memory** (IndexCache): Parsed `*repo.IndexFile` objects, LRU eviction, TTL expiration
2. **On-disk** (repository/): Downloaded YAML files, checked for staleness (5 min)
3. **Network**: Download if not cached or stale

## Implementation Tasks

### Task 1: Create HTTPChartSource - Complete HTTP Repository Abstraction

**File**: `internal/controller/helm/http_chart_source.go` (NEW, ~300-400 lines)

**Struct**:

```go
type HTTPChartSource struct {
    indexCache      *IndexCache
    basePath        string
    repoConfigPath  string
    repoCachePath   string
    refreshInterval time.Duration
}
```

**Public API**:

```go
func NewHTTPChartSource(basePath string, cacheSize int, cacheTTL, refreshInterval time.Duration) (*HTTPChartSource, error)

func (s *HTTPChartSource) GetChart(repoName, repoURL, chartName, version string, settings *cli.EnvSettings) (*chart.Chart, error)
```

**Private Methods**:

```go
func (s *HTTPChartSource) ensureRepository(repoName, repoURL string) error
func (s *HTTPChartSource) loadOrDownloadIndex(repoName, repoURL string) (*repo.IndexFile, error)
func (s *HTTPChartSource) loadChartFromIndex(index *repo.IndexFile, chartName, version string, settings *cli.EnvSettings) (*chart.Chart, error)
func (s *HTTPChartSource) isIndexStale(repoName string) bool
```

**Implementation Pseudocode**:

```go
// NewHTTPChartSource
NewHTTPChartSource(...) {
    absPath := filepath.Abs(basePath)
    repoCachePath := filepath.Join(absPath, "repository")
    os.MkdirAll(repoCachePath, 0755)
    
    return &HTTPChartSource{
        indexCache:      NewIndexCache(cacheSize, cacheTTL),
        basePath:        absPath,
        repoConfigPath:  filepath.Join(absPath, "repositories.yaml"),
        repoCachePath:   repoCachePath,
        refreshInterval: refreshInterval,
    }
}

// GetChart - orchestrates the entire flow
GetChart(repoName, repoURL, chartName, version, settings) {
    ensureRepository(repoName, repoURL)
    
    if index, found := indexCache.Get(repoName); found {
        return loadChartFromIndex(index, chartName, version, settings)
    }
    
    index := loadOrDownloadIndex(repoName, repoURL)
    indexCache.Set(repoName, repoURL, index)
    
    return loadChartFromIndex(index, chartName, version, settings)
}

// ensureRepository - migrate from repository.go:ensureRepository()
ensureRepository(repoName, repoURL) {
    // Use flock on repoConfigPath
    // Load or create repositories.yaml (repo.LoadFile / repo.NewFile)
    // Update or add repo entry
    // Write back to disk
}

// loadOrDownloadIndex
loadOrDownloadIndex(repoName, repoURL) {
    indexPath := filepath.Join(repoCachePath, repoName + "-index.yaml")
    
    if file exists and not stale {
        return repo.LoadIndexFile(indexPath)
    }
    
    // Download index from repoURL
    // Save to indexPath
    return repo.LoadIndexFile(indexPath)
}

// loadChartFromIndex
loadChartFromIndex(index, chartName, version, settings) {
    // Search index.Entries[chartName] for version
    // Get chart URL from entry
    // Download chart to temp file
    return loader.Load(tempFile)
}
```

**Code to Migrate**: Move logic from `repository.go:ensureRepository()` (lines ~107-180) into `HTTPChartSource.ensureRepository()`

### Task 2: Create IndexCache Implementation

**File**: `internal/controller/helm/index_cache.go` (NEW, ~200-250 lines)

**Structs**:

```go
type CachedIndex struct {
    Index      *repo.IndexFile
    CachedAt   time.Time
    AccessedAt time.Time  // For LRU tracking
    RepoURL    string
}

type IndexCache struct {
    mu      sync.RWMutex
    items   map[string]*CachedIndex
    maxSize int
    ttl     time.Duration
    stopCh  chan struct{}
}
```

**Public Methods**:

```go
func NewIndexCache(maxSize int, ttl time.Duration) *IndexCache
func (c *IndexCache) Get(repoName string) (*repo.IndexFile, bool)
func (c *IndexCache) Set(repoName string, repoURL string, index *repo.IndexFile) error
func (c *IndexCache) Clear()
func (c *IndexCache) Close()
```

**Implementation Pseudocode**:

```go
// Get - check cache and TTL
Get(repoName) {
    c.mu.RLock()
    cached := c.items[repoName]
    c.mu.RUnlock()
    
    if cached == nil || isExpired(cached) {
        return nil, false
    }
    
    c.mu.Lock()
    cached.AccessedAt = time.Now()  // Update for LRU
    c.mu.Unlock()
    
    return cached.Index, true
}

// Set - add with LRU eviction
Set(repoName, repoURL, index) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    if maxSize > 0 && len(items) >= maxSize {
        evictOldest()  // Find item with oldest AccessedAt and delete
    }
    
    c.items[repoName] = &CachedIndex{
        Index:      index,
        CachedAt:   time.Now(),
        AccessedAt: time.Now(),
        RepoURL:    repoURL,
    }
}

// Background cleanup goroutine (started in NewIndexCache)
startCleanup() {
    ticker := time.NewTicker(10 * time.Minute)
    go func() {
        for {
            select {
            case <-ticker.C:
                c.mu.Lock()
                for k, v := range c.items {
                    if isExpired(v) {
                        delete(c.items, k)
                    }
                }
                c.mu.Unlock()
            case <-c.stopCh:
                return
            }
        }
    }()
}
```

**Key Behaviors**:

- maxSize = 0: All operations are no-ops (cache disabled)
- LRU eviction: Evict by oldest AccessedAt (not CachedAt)
- TTL expiration: Delete expired items every 10 minutes in background

### Task 3: Integrate HTTPChartSource into Factory and Operations

**File**: `internal/controller/helm/operations.go`

**Changes**:

```go
// HelmOperationsFactory - replace fields
type HelmOperationsFactory struct {
    httpChartSource *HTTPChartSource  // NEW: replaces helmCacheBaseDir and helmIndexRefreshInterval
}

// NewHelmOperationsFactory - create singleton
func NewHelmOperationsFactory() (*HelmOperationsFactory, error) {
    cacheSize := getEnvOrDefaultInt("HELM_INDEX_CACHE_SIZE", 10)
    cacheTTL := getEnvOrDefaultDuration("HELM_INDEX_CACHE_TTL", 1*time.Hour)
    
    chartSource, err := NewHTTPChartSource("./helm", cacheSize, cacheTTL, 5*time.Minute)
    if err != nil {
        return nil, err
    }
    
    return &HelmOperationsFactory{httpChartSource: chartSource}, nil
}

// HelmOperations - add field
type HelmOperations struct {
    // ... existing fields ...
    chartSource *HTTPChartSource  // NEW: reference to singleton
}

// NewOperations - pass reference
func (f *HelmOperationsFactory) NewOperations(...) {
    // ... existing setup ...
    return &HelmOperations{
        // ... existing fields ...
        chartSource: f.httpChartSource,  // NEW
    }
}
```

### Task 4: Replace loadHelmChart with HTTPChartSource

**File**: `internal/controller/helm/operations_deploy.go`

**Replace**:

```go
chart, err := loadHelmChart(h.config, h.settings)
```

**With**:

```go
chart, err := h.chartSource.GetChart(
    h.config.Repository.Name,
    h.config.Repository.URL,
    h.config.Chart.Name,
    h.config.Chart.Version,
    h.settings,
)
```

**File**: `internal/controller/helm/repository.go`

**Delete**:

- `loadHelmChart()` function
- `setupHelmRepository()` function  
- `ensureRepository()` function (logic migrated to HTTPChartSource)

**Move to http_chart_source.go**:

- Constants: `helmRepositoriesFile`, `helmRepositoriesLock`, `helmCacheDir`

### Task 5: Add Observability and Configuration

**Configuration** (already implemented in Task 3):

Environment variables:

- `HELM_INDEX_CACHE_SIZE` - Max number of indexes (default: 10, 0 = disabled)
- `HELM_INDEX_CACHE_TTL` - Time to live (default: "1h", examples: "30m", "2h")

Examples:

```bash
# Disabled
HELM_INDEX_CACHE_SIZE=0

# Small cache (3 repos, 30 min TTL)
HELM_INDEX_CACHE_SIZE=3
HELM_INDEX_CACHE_TTL=30m

# Large cache (20 repos, 2 hour TTL)
HELM_INDEX_CACHE_SIZE=20
HELM_INDEX_CACHE_TTL=2h
```

**Observability**:

Add logging in HTTPChartSource and IndexCache:

- Cache hit: `log.V(1).Info("helm index cache hit", "repo", repoName)`
- Cache miss: `log.V(1).Info("helm index cache miss", "repo", repoName)`
- Eviction: `log.Info("helm index cache eviction", "repo", evictedRepo, "reason", "size limit")`
- TTL expiry: `log.V(1).Info("helm index cache expired", "repo", repoName, "age", age)`
- Startup: `log.Info("HTTP chart source initialized", "cacheSize", cacheSize, "cacheTTL", cacheTTL)`

Optional CacheStats struct for future metrics:

```go
type CacheStats struct {
    Hits      int64
    Misses    int64
    Evictions int64
    Size      int
}
```

**Future**: Prometheus metrics (out of scope for MVP)

## Testing Strategy

### Unit Tests

**Test IndexCache** (`index_cache_test.go`):

- GetSet: Basic operations
- LRU: Fill cache beyond maxSize, verify oldest evicted
- TTL: Set item, wait for expiry, verify miss
- Disabled: maxSize=0, verify no-ops
- Concurrent: Run with `-race` flag

**Test HTTPChartSource** (`http_chart_source_test.go`):

- Mock HTTPChartSource with in-memory cache
- Verify GetChart() reuses cached indexes
- Verify cache miss triggers download

### Integration Tests

Use existing tests in `deployment-operator-tests`:

- Deploy 10 components from same repo → verify only 1 index download
- Verify charts deploy correctly with caching enabled

## Rollout Plan

### Phase 1: Implementation

1. Create `http_chart_source.go` with HTTPChartSource (complete HTTP abstraction)
2. Create `index_cache.go` with IndexCache (in-memory caching)
3. Update `operations.go` to create and use HTTPChartSource singleton
4. Update `operations_deploy.go` to call `chartSource.GetChart()`
5. Refactor/remove old code from `repository.go`
6. Add logging throughout

### Phase 2: Testing

1. Write unit tests for IndexCache (`index_cache_test.go`)
2. Write unit tests for HTTPChartSource (`http_chart_source_test.go`)
3. Run existing integration tests (should pass unchanged)
4. Performance testing: Deploy 10 components from same repo, measure improvement

### Phase 3: Documentation

1. Update `internal/controller/helm/README.md`:
   - Document HTTPChartSource architecture
   - Explain caching behavior
   - Show configuration options
2. Document environment variables (`HELM_INDEX_CACHE_SIZE`, `HELM_INDEX_CACHE_TTL`)
3. Add troubleshooting guide for cache issues

## Expected Impact

### Performance Improvements

**Without Cache** (10 deployments, same repo):

- Total allocations: 35GB
- Parsing time: 20 seconds
- GC cycles: ~200
- CPU overhead: High

**With Cache** (10 deployments, same repo):

- Initial allocation: 3.5GB (first deployment)
- Subsequent allocations: Minimal
- Parsing time: 2 seconds (first deployment only)
- GC cycles: ~20 (first deployment only)
- CPU overhead: Minimal after first deployment

**Memory Usage**:

- Cache overhead: ~40MB per repository
- 10 cached repos: ~400MB (acceptable)
- 20 cached repos: ~800MB (still reasonable)

### Risk Mitigation

**Risks**:

1. Memory exhaustion from too many cached indexes
   - **Mitigation**: LRU eviction with configurable max size
2. Stale index data (outdated chart versions)
   - **Mitigation**: TTL-based expiration (default 1 hour)
3. Thread safety issues
   - **Mitigation**: RWMutex, unit tests with race detector
4. Cache disabled breaks functionality
   - **Mitigation**: nil cache = no-op, existing behavior unchanged

## Implementation Notes

### Files to Create

1. `http_chart_source.go` - HTTPChartSource struct and methods (~300-400 lines)
2. `index_cache.go` - IndexCache struct and methods (~200-250 lines)

### Files to Modify

1. `operations.go` - Factory owns HTTPChartSource singleton
2. `operations_deploy.go` - Call `h.chartSource.GetChart()`
3. `repository.go` - Delete functions, move constants

### Code Migration

**From `repository.go:ensureRepository()` (lines ~107-180)**:

- Move to `HTTPChartSource.ensureRepository()`
- Keep flock logic identical
- Adapt to use HTTPChartSource fields

### Critical Behaviors

- **Cache disabled** (size=0): All operations must be no-ops, GetChart() works without cache
- **Thread safety**: RWMutex in IndexCache, flock for repositories.yaml
- **LRU eviction**: By AccessedAt (most recent access), not CachedAt
- **TTL expiration**: Background goroutine cleans every 10 minutes
- **Error handling**: Cache errors are warnings, download errors are fatal

### Environment Variables

- `HELM_INDEX_CACHE_SIZE` - default 10 (0 = disabled)
- `HELM_INDEX_CACHE_TTL` - default "1h" (parse to time.Duration)

## Success Criteria

✅ Cache implementation is thread-safe (passes race detector)  
✅ Configurable via environment variables  
✅ Gracefully handles disabled state (size=0)  
✅ Integration tests pass with cache enabled  
✅ Performance improvement visible in multi-deployment scenarios  
✅ Memory usage stays within bounds (LRU eviction works)  
✅ Logging provides visibility into cache behavior
