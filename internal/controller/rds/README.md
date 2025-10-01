# RDS Controller

This package contains the Kubernetes controller responsible for handling `Component` resources with `spec.handler: "rds"`.

## Purpose

The RDS controller manages the deployment and lifecycle of AWS RDS instances based on Component specifications. It implements the component handler interface for RDS-based database deployments using the generic controller base architecture.

## Architecture

The RDS controller uses the generic controller base architecture with RDS-specific operations:

- **`controller.go`** - Main controller setup using generic base controller  
- **`operations.go`** - RDS operations interface implementation and configuration
- **`operations_deploy.go`** - RDS database deployment and status checking operations
- **`operations_delete.go`** - RDS database deletion and cleanup operations
- **`*_test.go`** - Test suites for controller functionality

## Controller Logic

- **Filtering**: Only processes Components where `spec.handler == "rds"`
- **Protocol Compliance**: Uses generic base controller for claiming, status transitions, and finalizer management
- **Operations**: Implements RDS-specific deployment, upgrade, and deletion operations via AWS RDS SDK
- **Status Reporting**: Reports database status back to Component resource with detailed conditions
- **Enhanced Orchestration**: Supports timeout compliance, TerminationFailed state handling, and handler status coordination

## Implementation Status

**Current State**: This controller uses the generic base controller architecture but RDS operations contain placeholder implementations.

**TODO Items**:

- Complete AWS RDS SDK integration for database operations
- Add RDS configuration parsing and validation  
- Implement actual deployment, upgrade, and deletion operations
- Add comprehensive error handling for AWS API interactions
- Implement proper credential management and region selection
- Add timeout compliance for Component-configured deployment and termination timeouts
- Implement TerminationFailed state handling for permanent cleanup failures
- Add handler status coordination for RDS instance metadata persistence

## Configuration

Component configuration for RDS deployments is passed through the `spec.config` field. The exact schema is being designed but will include:

- **Engine**: Database engine (postgres, mysql, etc.)
- **Instance Class**: RDS instance size and type
- **Storage**: Storage configuration and sizing
- **Networking**: VPC, subnet, security group settings
- **Backup**: Backup and maintenance window settings
- **Credentials**: Database username and password configuration

**Note**: Timeout behavior will be controlled by Component-level timeout fields (`spec.deploymentTimeout` and `spec.terminationTimeout`) rather than RDS-specific configuration.

## Dependencies

- Generic base controller (`github.com/rinswind/deployment-operator/componentkit/controller`) - **Currently Used**
- AWS SDK for Go (v2) - **To Be Added**
- AWS RDS service client - **To Be Added**
- `sigs.k8s.io/controller-runtime` - Controller framework
- Component CRD from `deployment-operator`

## Implementation Details

The controller implements the three core protocols through the generic base controller:

1. **Claiming Protocol** - Uses handler-specific finalizers for atomic resource discovery
2. **Creation Protocol** - Immediate resource creation with status-driven progression  
3. **Deletion Protocol** - Finalizer-based deletion coordination with proper cleanup

All RDS operations are designed to be non-blocking and idempotent, with comprehensive status reporting and error handling once the AWS SDK integration is complete.

### Enhanced Orchestration Features (To Be Implemented)

**Timeout Compliance:**

- Must respect Component-configured `deploymentTimeout` for RDS instance creation operations
- Must respect Component-configured `terminationTimeout` for RDS instance deletion operations
- Monitor operation duration and fail appropriately when timeouts are exceeded

**TerminationFailed State Handling:**

- Handle permanent cleanup failures (e.g., RDS deletion constraints, dependency issues)
- Support retry annotation mechanism for failed deletion operations
- Transition to TerminationFailed state when cleanup cannot be completed

**Handler Status Coordination:**

- Use `status.handlerStatus` field to persist RDS instance metadata across reconciliation cycles
- Store instance IDs, connection endpoints, and configuration hashes
- Maintain deployment context for complex RDS operations and monitoring
