# IAM Policy Handler

Component handler for managing AWS IAM managed policies as standalone, reusable resources.

## Purpose

Create and manage IAM policies that can be referenced by multiple IAM roles, following AWS best practices for policy reuse and the small, focused components architecture principle.

## Configuration

### Required Fields

- **policyName**: IAM policy name (must be unique within AWS account and path)
- **policyDocument**: IAM policy document as JSON string

### Optional Fields

- **description**: Human-readable policy description
- **path**: IAM path prefix (defaults to "/")
- **tags**: Key-value map of AWS resource tags

## Usage Examples

### Basic Policy

```yaml
apiVersion: deployments.deployment-orchestrator.io/v1alpha1
kind: Component
metadata:
  name: secrets-reader-policy
spec:
  handler: iam-policy
  config:
    policyName: "eks-demo-secrets-reader"
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

### Policy with Description and Tags

```yaml
apiVersion: deployments.deployment-orchestrator.io/v1alpha1
kind: Component
metadata:
  name: s3-access-policy
spec:
  handler: iam-policy
  config:
    policyName: "eks-demo-s3-bucket-access"
    description: "Read and write access to application S3 buckets"
    policyDocument: |
      {
        "Version": "2012-10-17",
        "Statement": [
          {
            "Effect": "Allow",
            "Action": [
              "s3:GetObject",
              "s3:PutObject",
              "s3:DeleteObject"
            ],
            "Resource": "arn:aws:s3:::my-app-bucket/*"
          },
          {
            "Effect": "Allow",
            "Action": ["s3:ListBucket"],
            "Resource": "arn:aws:s3:::my-app-bucket"
          }
        ]
      }
    tags:
      Environment: "production"
      Application: "my-app"
      ManagedBy: "deployment-operator"
```

### Referencing in IAM Roles

```yaml
# First create the policy
apiVersion: deployments.deployment-orchestrator.io/v1alpha1
kind: Component
metadata:
  name: app-secrets-policy
spec:
  handler: iam-policy
  config:
    policyName: "eks-demo-app-secrets"
    policyDocument: |
      {
        "Version": "2012-10-17",
        "Statement": [{
          "Effect": "Allow",
          "Action": ["secretsmanager:GetSecretValue"],
          "Resource": "arn:aws:secretsmanager:*:*:secret:app/*"
        }]
      }
---
# Then reference it in an IAM role
apiVersion: deployments.deployment-orchestrator.io/v1alpha1
kind: Component
metadata:
  name: app-service-role
spec:
  handler: iam-role
  config:
    roleName: "eks-demo-app-service"
    policies:
      - componentRef: "app-secrets-policy"  # References policy ARN from status
    trustPolicy: |
      {
        "Version": "2012-10-17",
        "Statement": [{
          "Effect": "Allow",
          "Principal": {
            "Federated": "arn:aws:iam::123456789012:oidc-provider/oidc.eks.region.amazonaws.com/id/EXAMPLE"
          },
          "Action": "sts:AssumeRoleWithWebIdentity"
        }]
      }
```

The `iam-role` handler resolves `componentRef` to the policy ARN from `status.handlerStatus.policyArn`.

## Handler Status

The handler reports policy information in `status.handlerStatus`:

- **policyArn**: Stable ARN for referencing (remains constant across updates)
- **policyId**: AWS-assigned unique identifier
- **policyName**: Policy name as created
- **currentVersionId**: Current default version (e.g., "v1", "v2")

## Policy Updates

- Policies are updated via AWS versioning (ARN never changes)
- AWS allows maximum 5 versions per policy
- Oldest non-default versions are automatically deleted when at limit
- Policy documents are compared before creating new versions (no-op if unchanged)

## AWS Permissions Required

See [docs/iam-policy-handler-permissions.md](../../../docs/iam-policy-handler-permissions.md) for detailed IAM permissions and Terraform integration examples.

Minimum required IAM actions:

```json
[
    "iam:CreatePolicy", 
    "iam:GetPolicy",
    "iam:ListPolicies", 
    "iam:CreatePolicyVersion",
    "iam:GetPolicyVersion",
    "iam:ListPolicyVersions",
    "iam:DeletePolicyVersion",
    "iam:DeletePolicy",
    "iam:TagPolicy",
    "iam:UntagPolicy"
]
```
