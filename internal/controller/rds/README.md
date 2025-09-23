# RDS Controller

This package contains the Kubernetes controller responsible for handling `Component` resources with `spec.handler: "rds"`.

## Purpose

The RDS controller manages the deployment and lifecycle of AWS RDS instances via Terraform based on Component specifications. It implements the component handler interface for RDS-based deployments.

## Controller Logic

- **Filtering**: Only processes Components where `spec.handler == "rds"`
- **Claiming**: Implements the claiming protocol to ensure exclusive ownership
- **Deployment**: Manages RDS instance creation and updates via Terraform
- **Status**: Reports deployment status back to the Component resource

## Configuration

Component configuration for RDS deployments is passed through the `spec.config` field and typically includes:

- **Engine**: Database engine (postgres, mysql, etc.)
- **Instance Class**: RDS instance size
- **Storage**: Storage configuration
- **Networking**: VPC, subnet, security group settings
- **Backup**: Backup and maintenance window settings

## Dependencies

- Terraform provider for AWS RDS
- AWS SDK for Go
- `sigs.k8s.io/controller-runtime` - Controller framework
- Component CRD from `deployment-operator`

## Terraform Integration

This controller uses Terraform as the backend for RDS provisioning, ensuring:
- Consistent infrastructure-as-code practices
- State management and drift detection
- Integration with existing Terraform workflows
