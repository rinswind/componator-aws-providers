# Helm Handler Timeout Implementation Plan

## Overview

Implement timeout functionality in the Helm handler to prevent infinite deployment loops and provide operational visibility for deletion operations. Uses component-level timeout configuration with controller defaults.

## Implementation Strategy

**Two timeout types:**

1. **Deployment timeout**: Actionable - transitions to Failed for retry capability
2. **Deletion timeout**: Informational - updates status only, never blocks deletion

**Configuration approach:** Component-level timeouts in `spec.config.timeouts` with controller defaults as fallback.

## Phase 1: Extend HelmConfig Structure

**File**: `internal/controller/helm/config.go`

**Add timeout configuration to HelmConfig:**

```go
type HelmConfig struct {
    // ... existing fields
    ReleaseName      string          `json:"releaseName" validate:"required"`
    Repository       Repository      `json:"repository"`
    Chart           HelmChart       `json:"chart"`
    Values          map[string]any  `json:"values,omitempty"`
    
    // New timeout configuration (optional)
    Timeouts        *TimeoutConfig  `json:"timeouts,omitempty"`
}

type TimeoutConfig struct {
    // Deployment timeout - how long to wait for Helm release to become ready
    // Transitions to Failed when exceeded
    Deployment      *Duration       `json:"deployment,omitempty"`
    
    // Deletion timeout - informational threshold for deletion visibility
    // Updates status message only, never blocks deletion
    Deletion        *Duration       `json:"deletion,omitempty"`
}

// Duration wraps time.Duration with JSON marshaling support
type Duration struct {
    time.Duration
}

func (d *Duration) UnmarshalJSON(data []byte) error {
    var s string
    if err := json.Unmarshal(data, &s); err != nil {
        return err
    }
    
    dur, err := time.ParseDuration(s)
    if err != nil {
        return err
    }
    
    d.Duration = dur
    return nil
}
```

## Phase 2: Update Controller Structure

**File**: `internal/controller/helm/controller.go`

**Add default timeouts to ComponentReconciler:**

```go
type ComponentReconciler struct {
    client.Client
    Scheme              *runtime.Scheme
    claimValidator      *util.ClaimingProtocolValidator
    requeuePeriod       time.Duration
    
    // Default timeout configurations
    defaultDeploymentTimeout    time.Duration
    defaultDeletionTimeout     time.Duration
}
```

**Update SetupWithManager to configure defaults:**

```go
func (r *ComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
    r.Client = mgr.GetClient()
    r.Scheme = mgr.GetScheme()
    r.claimValidator = util.NewClaimingProtocolValidator(HandlerName)
    
    // Configure timeouts from environment variables with defaults
    r.requeuePeriod = parseTimeoutEnv("HELM_REQUEUE_PERIOD", 10*time.Second)
    r.defaultDeploymentTimeout = parseTimeoutEnv("HELM_DEPLOYMENT_TIMEOUT", 15*time.Minute)
    r.defaultDeletionTimeout = parseTimeoutEnv("HELM_DELETION_TIMEOUT", 30*time.Minute)
    
    // ... rest of setup
}

func parseTimeoutEnv(envVar string, defaultValue time.Duration) time.Duration {
    if value := os.Getenv(envVar); value != "" {
        if duration, err := time.ParseDuration(value); err == nil {
            return duration
        }
    }
    return defaultValue
}
```

## Phase 3: Timeout Resolution Logic

**File**: `internal/controller/helm/controller.go` (add new helper method)

**Add helper method to get effective timeouts:**

```go
// getEffectiveTimeouts returns deployment and deletion timeouts for the component
// Uses component-level configuration if present, otherwise controller defaults
func (r *ComponentReconciler) getEffectiveTimeouts(component *deploymentsv1alpha1.Component) (time.Duration, time.Duration, error) {
    // Parse component config to check for timeouts
    config, err := resolveHelmConfig(component)
    if err != nil {
        return 0, 0, err
    }
    
    // Start with controller defaults
    deploymentTimeout := r.defaultDeploymentTimeout
    deletionTimeout := r.defaultDeletionTimeout
    
    // Override with component-level timeouts if specified
    if config.Timeouts != nil {
        if config.Timeouts.Deployment != nil {
            deploymentTimeout = config.Timeouts.Deployment.Duration
        }
        if config.Timeouts.Deletion != nil {
            deletionTimeout = config.Timeouts.Deletion.Duration
        }
    }
    
    return deploymentTimeout, deletionTimeout, nil
}
```

## Phase 4: Deployment Timeout Implementation

**File**: `internal/controller/helm/controller.go`

**Target**: `handleCreation()` method, Deploying phase section (around line 143)

**Add timeout check before readiness check:**

```go
// 3. If Deploying -> check deployment progress
if util.IsDeploying(component) {
    // Get effective timeouts for this component
    deploymentTimeout, _, err := r.getEffectiveTimeouts(component)
    if err != nil {
        log.Error(err, "failed to resolve component timeouts")
        return ctrl.Result{RequeueAfter: r.requeuePeriod}, nil
    }
    
    // Check deployment timeout before checking readiness
    if util.IsPhaseTimedOut(component, deploymentTimeout) {
        elapsed := util.GetPhaseElapsedTime(component)
        log.Error(nil, "deployment timed out", 
            "elapsed", elapsed, 
            "timeout", deploymentTimeout,
            "chart", "will-be-extracted-from-config")
            
        util.SetFailedStatus(component, HandlerName, 
            fmt.Sprintf("Deployment timed out after %v (timeout: %v)", 
                elapsed.Truncate(time.Second), deploymentTimeout))
        return ctrl.Result{}, r.Status().Update(ctx, component)
    }
    
    // Existing readiness check logic continues unchanged...
    rel, err := getHelmRelease(ctx, component)
    // ... rest of existing code
}
```

## Phase 5: Deletion Visibility Implementation

**File**: `internal/controller/helm/controller.go`

**Target**: `handleDeletion()` method, deletion progress section (around line 249)

**Add timeout visibility check:**

```go
// 2. If Terminating -> check deletion progress
deleted, err := checkHelmReleaseDeleted(ctx, component)
if err != nil {
    log.Error(err, "deletion failed")
    util.SetTerminatingStatus(component, HandlerName, err.Error())
    return ctrl.Result{}, r.Status().Update(ctx, component)
}

if !deleted {
    // Get effective timeouts for this component
    _, deletionTimeout, timeoutErr := r.getEffectiveTimeouts(component)
    if timeoutErr != nil {
        log.Error(timeoutErr, "failed to resolve component timeouts")
    } else {
        // Check deletion timeout for operational visibility only
        if util.IsPhaseTimedOut(component, deletionTimeout) {
            elapsed := util.GetPhaseElapsedTime(component)
            log.Warn("deletion taking longer than expected", 
                "elapsed", elapsed, 
                "timeout", deletionTimeout,
                "component", component.Name)
            
            // Update status message for operational visibility
            util.SetTerminatingStatus(component, HandlerName, 
                fmt.Sprintf("Cleanup in progress (%v elapsed, expected: <%v)", 
                    elapsed.Truncate(time.Second), deletionTimeout))
            if statusErr := r.Status().Update(ctx, component); statusErr != nil {
                log.Error(statusErr, "failed to update terminating status with timeout warning")
            }
        }
    }
    
    log.Info("Deletion in progress, checking again in 10 seconds")
    return ctrl.Result{RequeueAfter: r.requeuePeriod}, nil
}
```

## Phase 6: Configuration Tests

**File**: `internal/controller/helm/config_test.go`

**Add tests for timeout configuration:**

```go
It("should parse timeout configuration", func() {
    configJSON := `{
        "repository": {
            "url": "https://charts.bitnami.com/bitnami",
            "name": "bitnami"
        },
        "chart": {
            "name": "nginx",
            "version": "15.4.4"
        },
        "releaseName": "test-nginx",
        "timeouts": {
            "deployment": "10m",
            "deletion": "5m"
        }
    }`
    
    // Test parsing logic...
})

It("should handle missing timeout configuration", func() {
    // Test that missing timeouts field doesn't break parsing
})
```

## Phase 7: Sample Updates

**Files**: `config/samples/component_*.yaml`

**Add timeout examples to complex samples:**

```yaml
# config/samples/component_postgres_example.yaml
apiVersion: deployment-orchestrator.io/v1alpha1
kind: Component
metadata:
  name: postgres-db
spec:
  handler: helm
  config:
    repository:
      name: bitnami
      url: https://repo.broadcom.com/bitnami-files
    chart:
      name: postgresql
      version: "12.12.10"
    releaseName: postgres-db
    timeouts:
      deployment: "15m"    # Long deployment for DB initialization
      deletion: "5m"       # Moderate deletion timeout
    values:
      auth:
        postgresPassword: "changeme123"
```

## Default Values

**Controller defaults (environment configurable):**

- `HELM_DEPLOYMENT_TIMEOUT`: 15 minutes
- `HELM_DELETION_TIMEOUT`: 30 minutes  
- `HELM_REQUEUE_PERIOD`: 10 seconds

**Component-level examples:**

- Simple apps (nginx): `deployment: "2m"`, `deletion: "1m"`
- Databases: `deployment: "15m"`, `deletion: "5m"`
- Complex stacks: `deployment: "30m"`, `deletion: "10m"`

## Key Design Principles

1. **Component-level flexibility with controller defaults**
2. **Deployment timeout is actionable** (transitions to Failed)
3. **Deletion timeout is informational only** (never blocks deletion)
4. **Backward compatibility** (timeouts field is optional)
5. **Clear timeout messages** in status and logs
6. **No aggressive deletion strategies** (safety first)

## Implementation Order

1. ✅ Phase 1: Extend HelmConfig (config structure) - **COMPLETED**
2. Phase 2: Update controller structure (defaults)
3. Phase 3: Add timeout resolution logic (helper method)
4. Phase 4: Implement deployment timeout (actionable)
5. Phase 5: Implement deletion visibility (informational)
6. Phase 6: Add tests
7. Phase 7: Update samples

## Implementation Status

### Phase 1: Extend HelmConfig Structure ✅ **COMPLETED**

**Implemented changes in `internal/controller/helm/config.go`:**

- ✅ Added `TimeoutConfig` struct with `Deployment` and `Deletion` timeout fields
- ✅ Added custom `Duration` type with JSON unmarshaling support via `UnmarshalJSON` method
- ✅ Extended `HelmConfig` struct with optional `Timeouts *TimeoutConfig` field
- ✅ Added `time` package import
- ✅ Maintained backward compatibility - timeouts field is optional
- ✅ All existing tests pass
- ✅ Code compiles successfully

**Implementation details:**
- Duration parsing supports standard Go duration format (e.g., "15m", "2h30m", "30s")
- TimeoutConfig fields are optional pointers to allow differentiation between unset and zero values
- Clear documentation distinguishes between actionable deployment timeout and informational deletion timeout
- Follows established code patterns and validation framework integration

## Success Criteria

- [ ] Components with deployment timeout transition to Failed when exceeded
- [ ] Failed components can be retried by updating spec
- [ ] Deletion timeout updates status messages but never blocks deletion
- [x] Component-level timeouts override controller defaults *(structure ready)*
- [x] Missing timeout config uses controller defaults *(structure ready)*
- [x] All existing functionality remains unchanged *(verified)*
- [ ] Tests validate timeout parsing and behavior
