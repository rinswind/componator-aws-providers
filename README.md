# AWS Infrastructure Component Providers

Kubernetes Component Handler Controllers for AWS infrastructure provisioning and management.

## Project Overview

This project contains AWS-focused controllers that handle different infrastructure types through the standardized Component resource interface:

- **IAM Policy Handler** (`internal/controller/iam-policy/`) - Create and manage AWS IAM policies
- **IAM Role Handler** (`internal/controller/iam-role/`) - Create and manage AWS IAM roles
- **Secret Push Handler** (`internal/controller/secret-push/`) - Push secrets to AWS Secrets Manager
- **RDS Handler** (`internal/controller/rds/`) - Provision and manage RDS database instances

Each handler claims and processes Component resources based on their `spec.handler` field, implementing the actual deployment logic while following standardized protocols.

## Architecture

- **componator**: Provides Composition Controller, CRD definitions, and complete handler toolkit (`componentkit/controller/`, `componentkit/util/`, `componentkit/simulator/`)
- **componator-aws-providers**: AWS infrastructure Component Handler Controllers
- **componator-k8s-providers**: Kubernetes-native Component Handler Controllers (separate project)

**Key Features:**
- ✅ AWS infrastructure as Kubernetes resources
- ✅ Native AWS SDK integration
- ✅ Secure credential management
- ✅ Declarative infrastructure provisioning

## Prerequisites

### AWS Credentials

The controller requires AWS credentials to provision infrastructure. Configure credentials using one of these methods:

1. **IAM Roles for Service Accounts (IRSA)** - Recommended for EKS:
   ```yaml
   serviceAccount:
     annotations:
       eks.amazonaws.com/role-arn: arn:aws:iam::ACCOUNT:role/componator-aws-providers
   ```

2. **Environment Variables**:
   ```bash
   AWS_ACCESS_KEY_ID=your-access-key
   AWS_SECRET_ACCESS_KEY=your-secret-key
   AWS_REGION=us-west-2
   ```

3. **EC2 Instance Profile** - For self-managed Kubernetes on EC2

## Building and Running

```bash
# Build the project
go build -o bin/manager cmd/main.go

# Run locally (requires AWS credentials configured)
./bin/manager

# Run with specific flags
./bin/manager --metrics-bind-address=:8080 --health-probe-bind-address=:8081
```

### Development

```bash
# Run tests
make test

# Install CRDs (requires the componator CRDs to be installed first)
make install

# Run locally
make run
```

## Dependencies

This project depends on:
- **componator** - CRD definitions and handler toolkit
- **AWS SDK for Go v2** - AWS service clients (IAM, RDS, Secrets Manager)

The dependency is managed via Go modules.

## Handler Details

### IAM Policy Handler
Creates and manages AWS IAM policies:
- Policy document templating
- Version management
- Policy attachment tracking

### IAM Role Handler
Creates and manages AWS IAM roles:
- Trust policy configuration
- Policy attachments
- Role assumption permissions

### Secret Push Handler
Pushes Kubernetes secrets to AWS Secrets Manager:
- Automatic secret rotation support
- Versioning
- Cross-region replication

### RDS Handler
Provisions and manages RDS instances:
- Multi-AZ deployments
- Automated backups
- Parameter group management
- Subnet group configuration

## Installation

```bash
# Build and push container image
make docker-build docker-push IMG=your-registry/componator-aws-providers:tag

# Deploy to cluster (ensure AWS credentials are configured)
kubectl apply -f config/default
```

## Security Considerations

- Use IRSA (IAM Roles for Service Accounts) when running on EKS
- Follow principle of least privilege for IAM permissions
- Store sensitive data in AWS Secrets Manager, not ConfigMaps
- Enable audit logging for AWS API calls

## See Also

- [componator](https://github.com/rinswind/componator) - Composition Controller and CRDs
- [componator-k8s-providers](https://github.com/rinswind/componator-k8s-providers) - Kubernetes-native providers
- [componator-tests](https://github.com/rinswind/componator-tests) - Integration tests
