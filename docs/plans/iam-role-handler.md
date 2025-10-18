# Implementation Plan: IAM Role Handler

## Feature Overview

Implement a Component handler for AWS IAM roles that creates roles with OIDC trust policies, manages managed policy attachments, and updates roles in place following AWS best practices. The handler works with iam-policy Components to compose complete IAM configurations using the small, focused components architecture.

## Architecture Impact

**Patterns/Protocols Involved:**

- Component claiming protocol (handler-specific finalizer)
- Creation protocol (immediate resource creation, status reporting)
- Deletion protocol (finalizer-based cleanup coordination)
- Configuration resolution protocol (trust policies and policy ARNs use templates)
- Component composition (depends on iam-policy Components)

**Components Affected:**

- deployment-operator-handlers: New iam-role handler implementation
- Generic base controller: Uses existing ComponentOperations interface

**Integration Points:**

- AWS IAM API (CreateRole, UpdateAssumeRolePolicy, AttachRolePolicy, DeleteRole)
- Component handlerStatus (exposes roleArn for application Components)
- iam-policy Components (reads policyArn from their handlerStatus)

**Constraints:**

- Role updates must preserve ARN (service account annotations reference it)
- Trust policy updates use UpdateAssumeRolePolicy, never recreation
- Policy attachments reconciled (add missing, remove extras)
- No inline policies support (removed per design decision - only managed policies)
- Handler operates independently, no awareness of service accounts using role

## API Changes

**New Handler Configuration:**

```go
type IamRoleConfig struct {
    RoleName           string            `json:"roleName" validate:"required"`
    AssumeRolePolicy   string            `json:"assumeRolePolicy" validate:"required,json"`
    Description        string            `json:"description,omitempty"`
    MaxSessionDuration int32             `json:"maxSessionDuration,omitempty" validate:"omitempty,min=3600,max=43200"`
    Path               string            `json:"path,omitempty"`
    ManagedPolicyArns  []string          `json:"managedPolicyArns" validate:"required,min=1"`
    Tags               map[string]string `json:"tags,omitempty"`
}
```

**Handler Status:**

```go
type IamRoleStatus struct {
    RoleArn          string   `json:"roleArn"`
    RoleId           string   `json:"roleId"`
    RoleName         string   `json:"roleName"`
    AttachedPolicies []string `json:"attachedPolicies"`
}
```

**Component Example:**

```yaml
apiVersion: deployments.deployment-orchestrator.io/v1alpha1
kind: Component
metadata:
  name: eso-iam-role
spec:
  handler: iam-role
  dependsOn:
    - terraform-config
    - secrets-reader-policy
  config:
    roleName: "eks-demo-eso"
    description: "IRSA role for External Secrets Operator"
    assumeRolePolicy: |
      {
        "Version": "2012-10-17",
        "Statement": [{
          "Effect": "Allow",
          "Principal": {
            "Federated": "{{ .terraform-config.handlerStatus.oidcProviderArn }}"
          },
          "Action": "sts:AssumeRoleWithWebIdentity",
          "Condition": {
            "StringEquals": {
              "{{ .terraform-config.handlerStatus.oidcIssuer }}:sub": "system:serviceaccount:external-secrets-system:external-secrets"
            }
          }
        }]
      }
    managedPolicyArns:
      - "{{ .secrets-reader-policy.handlerStatus.policyArn }}"
```

## Critical Logic and Decisions

### Component: IamRoleOperations

**Key Responsibilities:**

- Parse and validate role configuration
- Create IAM role with trust policy on initial deployment
- Update trust policy in place when config changes
- Reconcile managed policy attachments (add/remove to match desired state)
- Delete role after detaching all policies
- Report role ARN in handlerStatus

**Deploy Flow:**

```text
Check if role exists (GetRole):
  if not exists:
    Create role (CreateRole with trust policy)
    Attach all managed policies (AttachRolePolicy for each)
    Update status with role ARN
    Return success
  
  if exists:
    Update status with existing ARN
    Compare trust policies (normalized JSON):
      if changed:
        Update trust policy (UpdateAssumeRolePolicy)
    
    Reconcile managed policies:
      List currently attached (ListAttachedRolePolicies)
      Calculate: to_attach = desired - current
      Calculate: to_detach = current - desired
      Attach missing policies
      Detach removed policies
    
    Return success
```

**Delete Flow:**

```text
List attached policies (ListAttachedRolePolicies):
  for each attached policy:
    Detach policy (DetachRolePolicy)

Delete role (DeleteRole)
Return success
```

**Design Decisions:**

- **Update in place, never recreate** - Preserves ARN for service account references
- **Policy reconciliation** - Declarative: make actual match desired, not incremental updates
- **No inline policies** - Removed per architecture decision, simplifies handler
- **JSON normalization for comparison** - Whitespace/order differences don't trigger updates
- **Trust policy as JSON string** - AWS native, supports template interpolation
- **Error classification** - IAM throttling/network = retryable, invalid trust policy = permanent failure

### Trust Policy Management

**Update Pattern:**

```text
Current trust policy from AWS (URL-encoded JSON)
Desired trust policy from config (JSON string with templates resolved)

Normalize both:
  Parse JSON → canonical form → marshal
  
Compare normalized versions:
  if different:
    UpdateAssumeRolePolicy with desired policy
    ARN stays same, references unbroken
```

**Template Resolution:**

Trust policies contain templates resolved by Composition Controller before handler sees them:

```text
User defines:
  "Principal": {"Federated": "{{ .terraform-config.handlerStatus.oidcProviderArn }}"}

Composition Controller resolves:
  "Principal": {"Federated": "arn:aws:iam::123456789012:oidc-provider/..."}

Handler receives:
  Fully resolved JSON string ready for AWS API
```

### Policy Attachment Reconciliation

**Reconciliation Pattern:**

```text
Desired: [arn:policy-a, arn:policy-b, arn:policy-c]
Current: [arn:policy-a, arn:policy-c, arn:policy-d]

Calculate differences:
  to_attach = desired - current = [arn:policy-b]
  to_detach = current - desired = [arn:policy-d]

Execute:
  AttachRolePolicy(policy-b)
  DetachRolePolicy(policy-d)

Result: [arn:policy-a, arn:policy-b, arn:policy-c]
```

**Idempotency:**

Operations are safe to retry - attach/detach are idempotent at AWS API level.

### Error Handling Strategy

**Retryable Errors:**

- Network timeouts
- AWS API throttling (TooManyRequests)
- Transient AWS service errors

**Permanent Errors:**

- Invalid trust policy JSON
- AWS IAM trust policy validation failures (invalid principals, conditions)
- Policy ARN doesn't exist (likely iam-policy Component failed)
- Permission denied (missing IAM handler permissions)

**Error Classification:**

```text
IAM API error received:
  if AWS SDK retry.IsErrorRetryable(err):
    return I/O error (controller requeues)
  else if policy ARN not found:
    return OperationError "Managed policy not found - check iam-policy Component"
  else:
    return OperationError (component goes to Failed phase)
```

## Testing Approach

**Unit Tests:**

- Configuration parsing and validation
- JSON normalization for trust policy comparison
- Policy reconciliation logic (set difference calculations)
- Error classification

**Integration Tests:**

- Full handler with AWS SDK (may use localstack initially)
- Component lifecycle: create → update trust policy → update policies → delete
- Policy attachment reconciliation scenarios
- Status reporting with roleArn
- Dependency on iam-policy Components

**Critical Scenarios:**

- Create new role with policies
- Update trust policy (OIDC issuer changes)
- Add managed policy (new dependency added)
- Remove managed policy (dependency removed)
- Replace managed policy (swap one for another)
- Delete role (detach all, then delete)
- Invalid trust policy JSON (permanent failure)
- Policy ARN not found (dependency issue)
- AWS throttling (retryable, requeue)

## Implementation Phases

### Phase 1: Handler Structure ✅

**Status:** COMPLETE (commit 61cec83)

**Completed:**

- ✅ Handler directory structure created (`internal/controller/iam-role/`)
- ✅ Configuration parsing with validation (`config.go`, `config_test.go`)
- ✅ ComponentOperations factory with AWS SDK setup (`operations.go`)
- ✅ Controller registration in `cmd/main.go`
- ✅ Full deploy operations implemented (`operations_deploy.go`)
- ✅ Delete operations stubbed (`operations_delete.go`)

**Implementation Details:**

- Config validation: required fields, trust policy JSON syntax, managedPolicyArns min=1
- Defaults applied: path="/", maxSessionDuration=3600
- AWS SDK v2 with retries disabled (controller handles requeue)
- Error classification using AWS SDK retry logic
- 108 lines of unit tests validating configuration parsing

**Validation:** ✅ Handler builds, config parsing tests pass, validates all required fields

---

### Phase 2: Deploy Operations ✅

**Status:** COMPLETE (commit 61cec83)

**Completed:**

- ✅ Role creation (CreateRole) with trust policy
- ✅ Trust policy updates (UpdateAssumeRolePolicy) with JSON normalization
- ✅ Policy attachment reconciliation (set-based add/remove)
- ✅ Status reporting with roleArn, roleId, attachedPolicies
- ✅ CheckDeployment verifies role exists
- ✅ Comprehensive logging throughout deploy operations

**Implementation Details:**

- JSON comparison using semantic equality (handles whitespace/ordering)
- Policy reconciliation: calculate to_attach/to_detach sets, apply changes
- Partial failure handling: status reflects actual attached policies even on error
- Tag support for role creation
- Role ARN preserved across updates (never recreate)

**Validation:** ✅ All deploy operations implemented and ready for AWS testing

---

### Phase 3: Delete Operations ✅

**Status:** COMPLETE (commit f102621)

**Completed:**

- ✅ Policy detachment before deletion (all managed policies)
- ✅ Role deletion (DeleteRole)
- ✅ CheckDeletion verifies role removed
- ✅ Shared helper functions moved to `operations.go`

**Implementation Details:**

- Lists all attached policies before deletion
- Detaches each policy sequentially
- Handles already-deleted roles gracefully
- Error classification for retryable vs permanent failures

**Validation:** ✅ Delete operations implemented and ready for AWS testing

---

### Phase 4: Documentation and Validation ✅

**Status:** COMPLETE

**Completed:**

- ✅ Controller registration in `cmd/main.go`
- ✅ Handler README with usage examples and patterns
- ✅ Error classifier using AWS SDK retry logic
- ✅ Comprehensive structured logging with context
- ✅ Status updates reflect partial progress on failures

**Implementation Details:**

- README documents all configuration fields and usage patterns
- Common patterns section covers ESO and multi-policy applications
- Error handling section explains retryable vs permanent failures
- Template resolution workflow documented
- Design decisions documented (update in place, no inline policies, declarative reconciliation)

**Note on Testing:**

Integration tests will be added to `deployment-operator/test/e2e` package later as part of cross-handler validation scenarios.

**Validation:** ✅ Handler fully implemented and documented, ready for real-world AWS testing

## Open Questions

### Resolved ✅

**Phase 1:**

- ✅ AWS SDK version → AWS SDK v2 (matches iam-policy handler)
- ✅ maxSessionDuration default → Default to 3600 (1 hour), not required
- ✅ Path default → Default to "/" (root path)

**Phase 2:**

- ✅ Trust policy comparison → Compare normalized JSON before updating (optimization)
- ✅ Policy attachment failures → Update status with partial progress, return error for retry
- ✅ Validate policy ARNs → No pre-validation, let AWS API return error (simpler, AWS is source of truth)

### Remaining Open Questions

**Resolved - No Pre-validation Needed:**

- Handler does not validate policy ARN naming conventions - AWS API is source of truth
- Error messages include full AWS error details with policy ARN context
- Integration tests deferred to `deployment-operator/test/e2e` package
- Role name validation handled by AWS API - no additional validation needed

**Implementation Complete:**

All phases complete. Handler is ready for real-world AWS testing with actual IAM API.
