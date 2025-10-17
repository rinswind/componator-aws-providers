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

### Phase 1: Handler Structure

**Goals:**

- Handler directory structure created
- Configuration parsing implemented with validation
- ComponentOperations stubs with AWS SDK setup
- Controller registration in main.go

**Deliverables:**

- `internal/controller/iam-role/` directory with files
- Config validation including trust policy JSON validation
- Handler compiles and registers
- No AWS operations yet (stubs return not-implemented)

**Validation:** Handler builds, basic config parsing tests pass, validates managedPolicyArns required

---

### Phase 2: Deploy Operations

**Goals:**

- Implement role creation (CreateRole)
- Implement trust policy updates (UpdateAssumeRolePolicy)
- Implement policy attachment reconciliation
- Status reporting with roleArn

**Deliverables:**

- `operations_deploy.go` fully implemented
- CheckDeployment verifies role exists
- Policy reconciliation working (add/remove)
- Integration tests for create and update scenarios

**Validation:** Can create roles, update trust policies, reconcile policy attachments in AWS

---

### Phase 3: Delete Operations

**Goals:**

- Implement policy detachment before deletion
- Implement role deletion
- Verify deletion completion

**Deliverables:**

- `operations_delete.go` fully implemented
- CheckDeletion verifies role removed
- Integration tests for deletion with multiple policies

**Validation:** Roles deleted cleanly with all policies detached first

---

### Phase 4: Error Handling and Integration

**Goals:**

- Robust error classification
- Proper retry handling
- Logging and observability
- Integration with iam-policy handler

**Deliverables:**

- Error classifier using AWS SDK retry logic
- Comprehensive logging
- Handler README with examples showing iam-policy composition
- Integration tests with actual iam-policy Components

**Validation:** Handler resilient to errors, clear messages, works with iam-policy dependencies

## Open Questions

**Before Phase 1:**

- AWS SDK version (v2 is standard, matches iam-policy handler)
- Should maxSessionDuration have a default value or be required?
- Path default: "/" or require explicit configuration?

**Before Phase 2:**

- Should we always update trust policy or compare first (optimization vs simplicity)?
- How to handle policy attachment failures (partial success scenarios)?
- Should we validate policy ARNs exist before attempting attach?

**Before Phase 4:**

- Should handler validate that policy ARNs match cluster naming convention?
- Error message for missing policy ARN: suggest checking iam-policy Component status?
