# RDS Controller

This package contains the Kubernetes controller responsible for handling `Component` resources with `spec.handler: "rds"`.

## Purpose

The RDS controller manages the deployment and lifecycle of AWS RDS instances via the AWS RDS SDK based on Component specifications. It implements the component handler interface for RDS-based deployments.

## Controller Logic

- **Filtering**: Only processes Components where `spec.handler == "rds"`
- **Claiming**: Implements the claiming protocol to ensure exclusive ownership
- **Deployment**: Manages RDS instance creation and updates via AWS RDS SDK
- **Status**: Reports deployment status back to the Component resource

## Configuration

Component configuration for RDS deployments is passed through the `spec.config` field and typically includes:

- **Engine**: Database engine (postgres, mysql, etc.)
- **Instance Class**: RDS instance size
- **Storage**: Storage configuration
- **Networking**: VPC, subnet, security group settings
- **Backup**: Backup and maintenance window settings

## Dependencies

- AWS SDK for Go (v2)
- AWS RDS service client
- `sigs.k8s.io/controller-runtime` - Controller framework
- Component CRD from `deployment-operator`

## AWS RDS SDK Integration

This controller uses the AWS RDS SDK directly for RDS provisioning, providing:

- Direct AWS API interaction for RDS operations
- Efficient resource management and status monitoring
- Native AWS error handling and retry logic
- Integration with AWS IAM and VPC configurations
