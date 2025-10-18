# Proposal: ConfigMap Namespace Filtering for Config-Reader Handler

## Status: Deferred

## Problem

Config-reader watches all ConfigMaps cluster-wide. At scale (10k+ ConfigMaps), the controller-runtime cache consumes significant memory (~100-150MB) even though most ConfigMaps are never referenced by Components.

## Use Case

Organizations deliver external configuration (Terraform outputs) in dedicated namespaces. Config-reader Components only reference ConfigMaps from these known namespaces (e.g., `default`, `infrastructure`, `terraform-outputs`). Watching all ConfigMaps is wasteful.

## Solution

### Option 1: Namespace Filtering

CLI flag specifies namespaces for ConfigMap watching:

```bash
--config-namespaces=default,infrastructure,terraform-outputs
```

Manager configures cache to only watch specified namespaces:

```go
cacheOptions.ByObject = map[client.Object]cache.ByObject{
    &corev1.ConfigMap{}: {
        Namespaces: map[string]cache.Config{
            "default": {},
            "infrastructure": {},
        },
    },
}
```

### Option 2: Label-Based Filtering

CLI flag specifies label selector for ConfigMap watching:

```bash
--config-label-selector=config-source=terraform
```

Manager configures cache with label selector:

```go
cacheOptions.ByObject = map[client.Object]cache.ByObject{
    &corev1.ConfigMap{}: {
        Label: labels.SelectorFromSet(labels.Set{
            "config-source": "terraform",
        }),
    },
}
```

ConfigMaps must be labeled for discovery:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: terraform-outputs
  namespace: default
  labels:
    config-source: terraform
```

### Combined Filtering

Both options can be used together for double filtering (namespace AND label):

```bash
--config-namespaces=default,infrastructure \
--config-label-selector=config-source=terraform
```

```go
cacheOptions.ByObject = map[client.Object]cache.ByObject{
    &corev1.ConfigMap{}: {
        Namespaces: map[string]cache.Config{
            "default": {},
            "infrastructure": {},
        },
        Label: labels.SelectorFromSet(labels.Set{
            "config-source": "terraform",
        }),
    },
}
```

Only ConfigMaps matching BOTH namespace AND label are cached.

**Behavior:**

- Without flags: watches all ConfigMaps cluster-wide (current behavior)
- With namespace flag: watches only specified namespaces
- With label flag: watches only labeled ConfigMaps across all namespaces
- With both flags: watches only labeled ConfigMaps in specified namespaces
- Components referencing non-watched ConfigMaps fail with clear error

## Limitations

**No namespace pattern support:** Controller-runtime requires exact namespace names (no `terraform-*` wildcards)

**No dynamic updates:** Cache config is immutable - filter changes require controller restart (rolling update)

**Label option requires ConfigMap changes:** Using label-based filtering requires ConfigMap owners to add labels, which may break existing workflows or require coordination across teams

## Memory Impact

### Namespace Filtering

| Cluster ConfigMaps | Watched Namespaces | Memory (Current) | Memory (Filtered) | Savings |
|-------------------|-------------------|------------------|-------------------|---------|
| 10,000            | All               | 150MB            | 150MB             | 0%      |
| 10,000            | 3                 | 150MB            | 52MB              | 65%     |
| 50,000            | 3                 | 550MB            | 55MB              | 90%     |
| 100,000           | 3                 | 1,050MB          | 60MB              | 94%     |

### Label Filtering

| Cluster ConfigMaps | Labeled ConfigMaps | Memory (Current) | Memory (Filtered) | Savings |
|-------------------|-------------------|------------------|-------------------|---------|
| 10,000            | All               | 150MB            | 150MB             | 0%      |
| 10,000            | 500               | 150MB            | 55MB              | 63%     |
| 50,000            | 1,000             | 550MB            | 60MB              | 89%     |
| 100,000           | 2,000             | 1,050MB          | 70MB              | 93%     |

### Combined Filtering (Namespace + Label)

Provides maximum savings - only ConfigMaps matching both filters are cached. Actual savings depend on overlap between namespace and label criteria.

**Key Insight:** Savings scale with cluster size. Combined filtering provides most control but requires label coordination.

## Future Enhancements

**Dynamic namespace discovery:** Watch Namespace resources with label selectors, update config, trigger rolling restart

**Namespace patterns:** Discover namespaces matching patterns at startup, periodically re-discover and restart if changed

**Admission validation:** Webhook validates Components only reference watched namespaces

## Decision

**Deferred** - Current cluster-wide behavior is pragmatic default. Implement when production demonstrates memory pressure or operators request it.
