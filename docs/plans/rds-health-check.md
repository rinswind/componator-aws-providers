# Implementation Plan: RDS Provider Health Check

## Feature Overview

Add runtime health monitoring to the RDS provider to detect operational degradation in Ready database instances. The health check will query AWS RDS instance status periodically and set the `Degraded` condition when instances enter problematic operational states (storage-full, maintenance, stopped) after deployment completes. This separates deployment completion verification (`checkApplied`) from ongoing operational health monitoring (`checkHealth`).

## Architecture Impact

**Patterns Involved:**

- Component health monitoring protocol (existing in componentkit)
- Functional provider registration with health checks

**Components Affected:**

- RDS provider (`componator-aws-providers/rds/`)
- Reuses existing AWS RDS client and status query infrastructure

**Integration Points:**

- Componentkit health check framework (`controller.ComponentHealthChecker`)
- Existing `getInstanceData()` function for RDS status queries
- Component Degraded condition management

**Key Constraints:**

- Health checks run only on Ready components (not during deployment)
- Must not transition component phase (only update Degraded condition)
- Must handle instance-not-found scenario (external deletion)
- AWS API rate limits (use 1-minute interval)

## API Changes

**New Functions:**

- `checkHealth(ctx, name, spec RdsConfig, status RdsStatus) (*controller.HealthCheckResult, error)`: Evaluates RDS instance operational health by querying AWS status

**Modified Functions:**

- `Register(mgr, providerName) error`: Add `.WithHealthCheck()` and `.WithHealthCheckInterval()` to builder chain

**New Constants/Types:**

None required - reuses existing `RdsConfig`, `RdsStatus`, `controller.HealthCheckResult`

## Critical Logic and Decisions

### Component: Health Check Evaluator (health.go)

Key responsibilities:

- Query current RDS instance status via AWS API
- Map AWS instance states to health status
- Return appropriate `HealthCheckResult`

State classification logic:

```text
query AWS for instance status:
  if instance not found:
    return Degraded(reason="InstanceDeleted", message="RDS instance not found in AWS")
  
  switch instance.Status:
    case "available", "storage-optimization", "backing-up",
         "configuring-enhanced-monitoring", "configuring-iam-database-auth", 
         "configuring-log-exports":
      return Healthy("Instance operational")
    
    case "storage-full":
      return Degraded("StorageFull", "Instance storage capacity exhausted")
    
    case "maintenance", "rebooting":
      return Degraded("Maintenance", "Instance undergoing maintenance")
    
    case "stopped", "stopping", "starting":
      return Degraded("Stopped", "Instance not running")
    
    case "failed", "inaccessible-encryption-credentials", 
         "incompatible-network", "incompatible-option-group",
         "incompatible-parameters", "incompatible-restore":
      return Degraded("Failed", "Instance in error state: {status}")
    
    default:
      return Degraded("UnknownStatus", "Instance in unknown state: {status}")
```

Design decisions:

- **Map failure states to Degraded (not Failed)**: Health checks don't trigger phase transitions; they only set conditions
- **Storage-full is Degraded**: Applications might recover by deleting data; not a permanent failure
- **Maintenance/rebooting are Degraded**: Temporary operational states, not deployment failures
- **Instance-not-found is Degraded**: Enables detection of external deletion while keeping Component in Ready phase

Error handling:

```text
if AWS API call fails:
  classify error via rdsErrorClassifier:
    if retryable (network, throttle):
      return (nil, error) -> controller requeues
    if non-retryable:
      return Degraded("APIError", error message)
```

### Component: checkApplied Refinement (operations.go)

Separate deployment-focused states from operational states:

Current issue: `checkApplied` treats some operational states as deployment completion criteria

Refinement:

```text
Ready states (deployment complete, DB usable):
  - available: fully operational
  - storage-optimization: post-creation optimization
  - backing-up: operational during backups
  - configuring-*: operational during feature enablement

Progressing states (deployment/modification in progress):
  - creating: initial provisioning
  - modifying: applying configuration changes
  - upgrading: engine version changes
  - renaming: identifier changes

Failed states (provisioning errors):
  - incompatible-*: configuration/compatibility issues
  - insufficient-capacity: AWS capacity issues
  - inaccessible-encryption-credentials: KMS access issues
```

Design decision:

- **Include backing-up in Ready**: Backups don't prevent connections; DB is operational
- **Include configuring-* in Ready**: Feature enablement doesn't require downtime
- **Keep modifying in Progressing**: Some modifications cause brief downtime; safer to wait
- **Move operational states to health check**: maintenance, rebooting, stopped

### Component: Health Check Registration (register.go)

Add health monitoring to provider registration:

```text
functional.NewBuilder[RdsConfig, RdsStatus](providerName).
  WithApply(applyAction).
  WithApplyCheck(checkApplied).
  WithDelete(deleteAction).
  WithDeleteCheck(checkDeleted).
  WithHealthCheck(checkHealth).              // NEW
  WithHealthCheckInterval(1 * time.Minute).  // NEW
  ...existing timeouts...
  Register(mgr)
```

Design decision:

- **1-minute interval**: Balances responsiveness vs AWS API limits; RDS state changes are not frequent
- **Opt-in via builder**: Uses existing functional provider pattern

## Testing Approach

**Unit Tests:**

- Health state classification logic (all AWS status mappings)
- Error handling (AWS API failures, missing instances)

**Integration/Manual Tests:**

- Deploy RDS Component to Ready state
- Stop instance via AWS console → verify Degraded=True appears
- Start instance → verify Degraded=False restored
- Trigger maintenance window → verify Degraded=True with Maintenance reason
- Monitor logs for health check execution frequency

**Critical Scenarios:**

- InstanceNotFound (external deletion detection)
- StorageFull (operational degradation)
- MaintenanceInProgress (scheduled events)
- HealthyAfterDegradation (recovery detection)
- ErrorClassification (transient vs permanent API errors)

## Implementation Phases

### Phase 1: Core Health Check Logic

- Create `health.go` with `checkHealth()` function
- Implement AWS status query via existing `getInstanceData()`
- Map all RDS instance states to health status (healthy/degraded)
- Add error classification for AWS API failures
- Validation: Unit tests pass for all state mappings

### Phase 2: Registration and Integration

- Modify `register.go` to add `.WithHealthCheck()` and `.WithHealthCheckInterval()`
- Configure 1-minute health check interval
- Validation: Health checker registered, logs show periodic execution

### Phase 3: Refine Deployment Checks

- Update `checkApplied` state categorization in `operations.go`
- Move `backing-up`, `configuring-*` to Ready states
- Ensure operational states handled by health checks, not deployment checks
- Validation: Deployment completion logic remains correct

### Phase 4: Testing and Validation

- Run integration tests against real RDS instances
- Verify Degraded condition appears/clears correctly
- Monitor AWS API call frequency (should be ~1/minute per instance)
- Document expected behaviors and troubleshooting
- Validation: Manual test scenarios complete successfully

## Open Questions

None - all design decisions can proceed with existing architecture and patterns.
