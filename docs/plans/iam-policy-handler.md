# Implementation Plan: IAM Policy Handler

## Feature Overview

Implement a Component handler for AWS IAM managed policies that creates, updates via versioning, and deletes IAM policies as standalone, reusable Components. The handler enables policy reuse across multiple IAM roles following AWS best practices and the small, focused components architecture principle.

## Architecture Impact

**Patterns/Protocols Involved:**

- Component claiming protocol (handler-specific finalizer)
- Creation protocol (immediate resource creation, status reporting)
- Deletion protocol (finalizer-based cleanup coordination)
- Configuration resolution protocol (policy names can use templates)

**Components Affected:**

- deployment-operator-handlers: New iam-policy handler implementation
- Generic base controller: Uses existing ComponentOperations interface

**Integration Points:**

- AWS IAM API (CreatePolicy, CreatePolicyVersion, DeletePolicy)
- Component handlerStatus (exposes policyArn for iam-role Components)

**Constraints:**

- IAM policies limited to 5 versions (requires version management)
- Policy updates must use versioning, never recreation (preserves ARN)
- Policy ARN must remain stable across updates (referenced by roles)
- Handler operates independently, no awareness of role attachments

## API Changes

**New Handler Configuration:**

```go
type IamPolicyConfig struct {
    PolicyName     string            `json:"policyName" validate:"required"`
    PolicyDocument string            `json:"policyDocument" validate:"required,json"`
    Description    string            `json:"description,omitempty"`
    Path           string            `json:"path,omitempty"`
    Tags           map[string]string `json:"tags,omitempty"`
}
```

**Handler Status:**

```go
type IamPolicyStatus struct {
    PolicyArn  string `json:"policyArn"`
    PolicyId   string `json:"policyId"`
    PolicyName string `json:"policyName"`
}
```

**Component Example:**

```yaml
apiVersion: deployments.deployment-orchestrator.io/v1alpha1
kind: Component
metadata:
  name: secrets-reader-policy
spec:
  handler: iam-policy
  config:
    policyName: "eks-demo-secrets-reader"
    description: "Read secrets from AWS Secrets Manager"
    policyDocument: |
      {
        "Version": "2012-10-17",
        "Statement": [{
          "Effect": "Allow",
          "Action": ["secretsmanager:GetSecretValue"],
          "Resource": "*"
        }]
      }
```

## Critical Logic and Decisions

### Component: IamPolicyOperations

**Key Responsibilities:**

- Parse and validate policy configuration
- Create IAM policy on initial deployment
- Update policy via versioning on config changes
- Delete policy and all versions on Component deletion
- Report policy ARN in handlerStatus

**Deploy Flow:**

```text
Check if policy exists (GetPolicy):
  if not exists:
    Create policy (CreatePolicy)
    Update status with policy ARN
    Return success
  
  if exists:
    Update status with existing ARN
    Create new policy version (CreatePolicyVersion with SetAsDefault=true)
    Handle version limit (delete oldest if at 5 versions)
    Return success
```

**Delete Flow:**

```text
List all policy versions (ListPolicyVersions):
  for each non-default version:
    Delete version (DeletePolicyVersion)
  
Delete policy (DeletePolicy)
Return success
```

**Design Decisions:**

- **Always update via versioning** - AWS native pattern, preserves ARN, enables rollback
- **Automatic version cleanup** - When at 5 versions, delete oldest non-default before creating new
- **No drift detection initially** - Policy updates always create new version (Phase 1 simplicity)
- **JSON string validation** - Validate JSON syntax, AWS validates policy semantics
- **Error classification** - IAM throttling/network = retryable, invalid policy = permanent failure

### AWS IAM Integration

**Policy Versioning Pattern:**

```text
Policy has 5 versions (at limit):
  CreatePolicyVersion called:
    if LimitExceeded error:
      List versions
      Find oldest non-default version
      Delete oldest version
      Retry CreatePolicyVersion
    else:
      AWS auto-assigns v6, deletes v1
```

**ARN Stability:**

```text
Policy ARN: arn:aws:iam::123456789012:policy/my-policy
                                                    ↑
                                Never changes across versions

Status reports this ARN → iam-role Components reference it → Stable across updates
```

### Error Handling Strategy

**Retryable Errors:**

- Network timeouts
- AWS API throttling (TooManyRequests)
- Transient AWS service errors

**Permanent Errors:**

- Invalid policy JSON syntax
- AWS IAM policy validation failures (invalid actions, malformed conditions)
- Permission denied (missing IAM handler permissions)

**Error Classification:**

```text
IAM API error received:
  if AWS SDK retry.IsErrorRetryable(err):
    return I/O error (controller requeues)
  else:
    return OperationError (component goes to Failed phase)
```

## Testing Approach

**Unit Tests:**

- Configuration parsing and validation
- JSON normalization for policy comparison
- Error classification (retryable vs permanent)

**Integration Tests:**

- Full handler with AWS SDK (may use localstack or mocks initially)
- Component lifecycle: create → update → delete
- Version management: verify version limit handling
- Status reporting: verify policyArn exposed correctly

**Critical Scenarios:**

- Create new policy
- Update existing policy (version creation)
- Update at version limit (cleanup flow)
- Delete policy with multiple versions
- Invalid policy JSON (permanent failure)
- AWS throttling (retryable, requeue)

## Implementation Phases

### Phase 1: Handler Structure ✅ COMPLETE

**Goals:**

- Handler directory structure created
- Configuration parsing implemented
- ComponentOperations stubs with AWS SDK setup
- Controller registration in main.go

**Deliverables:**

- `internal/controller/iam-policy/` directory with files
- Config validation and defaults working
- Handler compiles and registers
- No AWS operations yet (stubs return not-implemented)

**Validation:** Handler builds, basic config parsing tests pass

**Completion Notes:**

- Created: `config.go`, `operations.go`, `controller.go`, `config_test.go`
- All 16 unit tests passing
- Handler registered in `cmd/main.go`
- AWS SDK for IAM added to dependencies
- Region field removed - uses AWS config chain instead
- Stub operations return permanent failures (Phase 2 will implement)

---

### Phase 2: Deploy Operations ✅ COMPLETE

**Goals:**

- Implement policy creation (CreatePolicy)
- Implement policy updates via versioning (CreatePolicyVersion)
- Handle version limit cleanup
- Status reporting with policyArn

**Deliverables:**

- ✅ `operations_deploy.go` fully implemented
- ✅ CheckDeployment verifies policy exists
- ✅ Version management with automatic cleanup at 5-version limit
- ✅ Status reporting with policyArn, policyId, and versionId
- ✅ JSON normalization for policy comparison
- ✅ Helper functions for tags and error classification

**Validation:** Handler implements Deploy and CheckDeployment operations, e2e tests validate AWS integration

**Completion Notes:**

- Created: `operations_deploy.go`, `operations_deploy_test.go`
- Implements: CreatePolicy, CreatePolicyVersion, version limit cleanup
- Policy lookup by name (ListPolicies) and ARN (GetPolicy)
- Automatic cleanup of oldest non-default version when at 5 version limit
- Status updates with policyArn, policyId, policyName, currentVersionId
- Error handling for NoSuchEntityException (not found)
- Helper functions: convertToIAMTags, normalizeJSON, isNotFoundError
- All 24 unit tests passing
- Deploy and CheckDeployment ready for AWS integration testing

---

### Phase 3: Delete Operations

**Goals:**

- Implement policy deletion
- Handle version cleanup before deletion
- Verify deletion completion

**Deliverables:**

- `operations_delete.go` fully implemented
- CheckDeletion verifies policy removed
- Integration tests for deletion

**Validation:** Policies deleted cleanly, no orphaned resources

---

### Phase 4: Error Handling and Polish

**Goals:**

- Robust error classification
- Proper retry handling for AWS throttling
- Logging and observability
- Documentation

**Deliverables:**

- Error classifier using AWS SDK retry logic
- Comprehensive logging
- Handler README with examples
- Integration with terraform bootstrap (IAM handler permissions)

**Validation:** Handler resilient to AWS API errors, clear error messages in Component status

## Open Questions

**Before Phase 1:**

- AWS SDK version preference (v2 is standard for new code)
- LocalStack or AWS mocking strategy for integration tests?
- Should policy path default to "/" or require explicit configuration?

**Before Phase 2:**

- Should we compare policy documents before creating version (optimization) or always create (simpler)?
- Policy naming convention enforcement (cluster prefix required)?

**Before Phase 4:**

- Terraform IAM handler permissions location (new file or add to existing iam.tf)?
- Should handler support AWS managed policy references (read-only mode)?
