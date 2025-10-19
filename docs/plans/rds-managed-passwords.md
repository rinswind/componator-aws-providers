# Implementation Plan: RDS-Managed Password Support

## Feature Overview

Replace explicit password configuration with AWS RDS-managed master passwords in the RDS handler. AWS RDS automatically generates secure passwords, stores them in AWS Secrets Manager, and manages their lifecycle including optional rotation. The RDS handler captures the secret ARN and exposes it via Component status for downstream consumption by External Secrets Operator. This is a breaking change that removes insecure inline password support.

## Architecture Impact

**Protocols Involved:**

- Component Handler protocols (claiming, creation, deletion) - no changes needed
- Status coordination protocol - handler status used to expose secret ARN

**Components Affected:**

- RDS Handler: Config parsing, deployment operations, status management
- Component CRD: Uses existing `status.handlerStatus` field (no CRD changes)

**Integration Points:**

- AWS RDS API: `CreateDBInstance` and `DescribeDBInstances` with managed password parameters
- AWS Secrets Manager: RDS-created secrets (read-only from handler perspective)
- External Secrets Operator: Consumes secret ARN from Component status (external to handler)

**Trade-offs:**

- **Chosen**: RDS-managed passwords only (AWS generates and manages)
- **Removed**: Explicit inline password configuration
- **Rationale**: Eliminate insecure practices, enforce compliance, simpler implementation with single password management approach

## API Changes

**Modified Types:**

`RdsConfig` (in `config.go`):

- Add: `ManageMasterUserPassword *bool` - Enable RDS-managed password generation
- Add: `MasterUserSecretKmsKeyId string` - Optional KMS key for secret encryption
- Keep existing: `MasterUsername string` - Required username for database
- Remove: `MasterPassword string` - Explicit passwords no longer supported (breaking change)

`RdsStatus` (in `config.go`):

- Add: `MasterUserSecretArn string` - AWS Secrets Manager ARN for RDS-managed password

**Configuration Defaults:**

`applyRdsConfigDefaults()`:

- Default `ManageMasterUserPassword` to `true` (always)
- Validate: `MasterUsername` is provided
- Validate: Reject configuration if legacy `MasterPassword` field is present (breaking change)

## Critical Logic and Decisions

### Component: Configuration Resolution

Key responsibilities:

- Parse and validate credential configuration
- Apply secure defaults (RDS-managed passwords always)
- Reject legacy explicit password configurations

Critical validation:

```text
require MasterUsername (always)

if MasterPassword field is present:
  reject configuration with error: "Explicit passwords no longer supported. Remove masterPassword field - RDS will generate passwords automatically."

set ManageMasterUserPassword to true (always)
```

Design decisions:

- **Always managed**: All deployments use RDS-managed passwords (no option to disable)
- **Breaking change**: Existing configurations with explicit passwords must be updated
- **Clear error messages**: Provide migration guidance when legacy fields detected

### Component: Deployment Operations

Key responsibilities:

- Create RDS instance with managed password configuration
- Capture and persist secret ARN from AWS response
- Validate password configuration before API calls

Critical flow:

```text
on Deploy():
  validate credentials configuration
  build CreateDBInstance input
  
  set input.ManageMasterUserPassword = true (always)
  if KMS key configured:
    set input.MasterUserSecretKmsKeyId
  
  create RDS instance
  
  capture secret ARN from response.MasterUserSecret.SecretArn
  store in status.MasterUserSecretArn
  log secret ARN for observability
```

Design decisions:

- **Capture on create**: Secret ARN always present in CreateDBInstance response (RDS guarantees this)
- **Preserve in status**: Secret ARN persisted across reconciliation loops
- **Log but don't expose**: Log ARN for debugging, but actual password never logged
- **Fail if missing**: Error if secret ARN not present (indicates RDS API issue)

### Component: Status Management

Key responsibilities:

- Persist secret ARN across reconciliation cycles
- Expose secret ARN for external consumption via Component status

Critical flow:

```text
on CheckDeploymentStatus():
  describe RDS instance
  update standard status fields (endpoint, port, etc.)
  
  if instance has MasterUserSecret:
    preserve secret ARN in status.MasterUserSecretArn
    (ARN doesn't change once created)
```

Design decisions:

- **Immutable ARN**: Secret ARN doesn't change after instance creation
- **Always preserve**: Even if API response doesn't include it on subsequent calls
- **Status as contract**: ARN in status is source of truth for downstream systems

### Component: Error Handling

Error scenarios:

```text
Configuration validation errors:
  - Legacy MasterPassword field present
  - Missing MasterUsername
  → Fail fast with clear migration guidance

AWS API errors:
  - RDS creation fails: Standard error handling (retryable vs terminal)
  - Missing secret ARN in response: Fatal error (should never happen with managed passwords)
  → Use existing error classification patterns
```

Design decisions:

- **Strict validation**: Reject legacy password configurations with migration guidance
- **No graceful degradation**: Missing secret ARN is fatal - indicates RDS API contract violation
- **Standard retry**: Use existing RDS error classification for AWS errors

## Testing Approach

**Unit Tests:**

- Config parsing: Managed password always enabled, defaults applied
- Validation: Legacy password configurations rejected with helpful error messages
- Status serialization: Secret ARN preserved correctly

**Integration Tests:**

- RDS instance creation with managed password
- Secret ARN capture and status persistence (must be present)
- Legacy configuration rejection (breaking change validation)

**Critical Scenarios:**

- `RDS managed password creation` - End-to-end instance creation with password management
- `Secret ARN status persistence` - ARN survives reconciliation loops
- `Legacy password rejection` - Configurations with explicit passwords rejected with migration guidance
- `Missing secret ARN failure` - Fatal error if RDS doesn't return secret ARN

**External Integration:**

(Not part of RDS handler testing, but validates end-to-end flow)
- External Secrets Operator consuming ARN from Component status
- Kubernetes workloads accessing synced credentials

## Implementation Phases

**Phase 1: Configuration Support** ✅ **COMPLETE**

- ✅ Remove `MasterPassword` from `RdsConfig` (breaking change)
- ✅ Add `ManageMasterUserPassword` and `MasterUserSecretKmsKeyId` to `RdsConfig`
- ✅ Add `MasterUserSecretArn` to `RdsStatus`
- ✅ Implement validation that enforces managed password policy
- ✅ **Validation**: Config parsing tests pass, validation enforces managed password requirements

**Phase 2: Deployment Integration** ✅ **COMPLETE**

- ✅ Modify `Deploy()` to always use managed passwords (remove conditional logic)
- ✅ Capture secret ARN from `CreateDBInstance` response (required, fail if missing)
- ✅ Update `CheckDeploymentStatus()` to preserve secret ARN
- ✅ **Validation**: Deployment code uses managed password parameters, ARN captured in status

**Phase 3: Testing and Documentation** 🔄 **IN PROGRESS**

- ✅ Add unit tests for configuration validation and status parsing
- ⏳ Add integration tests for managed password lifecycle (requires real AWS)
- ⏳ Update RDS handler README with managed password documentation
- **Validation**: Unit tests pass, integration tests and documentation pending

**Phase 4: Example Manifests and Migration**

- Update all example Component manifests to remove explicit passwords
- Document integration pattern with External Secrets Operator
- Provide migration guide for existing deployments with inline passwords
- Create end-to-end WordPress example using managed passwords
- **Validation**: Example manifests deploy successfully, migration guide is clear

## Open Questions

**Breaking Change Impact:**

- Should this be implemented as a major version bump (v2.0.0)?
- Do we need a deprecation period or can we break immediately?
- Should we provide a migration tool/script for existing deployments?

**Migration Strategy:**

The breaking change requires existing RDS instances to be recreated (AWS doesn't support converting from explicit to managed passwords on existing instances). Options:

1. **Manual migration**: Users delete and recreate RDS instances
2. **Blue-green migration**: Create new instance with managed password, migrate data, switch over
3. **Automated migration**: Handler detects legacy config and guides user through migration

**Recommendation**: Document manual migration approach in Phase 3, consider automated tooling in future if demand exists.

## References

**AWS Documentation:**

- [RDS Managed Master Passwords](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/rds-secrets-manager.html)
- RDS API: `CreateDBInstance` with `ManageMasterUserPassword` parameter

**Related Components:**

- External Secrets Operator: Consumes secret ARN from Component status (external system)
- Component CRD: Existing `status.handlerStatus` field (no changes required)

**Codebase References:**

- `internal/controller/rds/config.go`: Configuration structures
- `internal/controller/rds/operations_deploy.go`: Deployment logic
- `internal/controller/rds/README.md`: Handler documentation
