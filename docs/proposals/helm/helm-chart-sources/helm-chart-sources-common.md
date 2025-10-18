# Chart Source Common Architecture

## Overview

This document describes the common architectural components shared across all chart source types in the Helm handler. The handler supports HTTP repositories, OCI registries, and Git repositories through a unified configuration interface with consistent security and parsing patterns.

## Configuration Parsing Architecture

**Two-Stage Parsing Architecture**: Configuration parsing uses a two-stage approach to handle type-specific schemas while maintaining validation and error handling consistency across all source types.

**Stage 1 - Type Detection**: Initial parsing extracts the source type to determine the appropriate parsing strategy for the remaining configuration.

**Stage 2 - Type-Specific Parsing**: Based on detected type, configuration is parsed into specialized structures that match each source type's natural addressing patterns.

## Common Configuration Elements

**Shared Structures**: All source types use common structures for credential references and base configuration:

```go
type SecretRef struct {
    Name      string `json:"name" validate:"required"`
    Namespace string `json:"namespace" validate:"required"`
}
```

**Base Configuration Structure**:

```go
type HelmConfig struct {
    // Common fields across all source types
    ReleaseName      string                 `json:"releaseName" validate:"required"`
    ReleaseNamespace string                 `json:"releaseNamespace" validate:"required"`
    ManageNamespace  *bool                  `json:"manageNamespace,omitempty"`
    Values           map[string]interface{} `json:"values,omitempty"`
    Timeouts         *HelmTimeouts          `json:"timeouts,omitempty"`
    
    // Source-specific configuration
    Source SourceConfig `json:"source" validate:"required"`
}

// SourceConfig is an interface implemented by all source types
type SourceConfig interface {
    GetType() string
    GetAuthentication() interface{}
}
```

## Security Architecture

### Credential Management

Credential management follows consistent patterns across all source types with specialized secret formats for different authentication methods.

**Secret Resolution Pattern**:

- **Namespace Scoping**: Credentials resolved within Component namespace first, fallback to system namespace
- **Secret Format**: Standardized secret keys for different authentication types
- **Rotation Support**: Handle credential rotation without service disruption

### Transport Security

Network security applies consistent policies across all source types with appropriate protocol-specific considerations.

**Security Policies**:

- **TLS Enforcement**: Require secure transport for all external communications
- **Certificate Validation**: Implement proper certificate chain validation
- **Proxy Support**: Support corporate proxy configurations for external access

**Access Control**:

- **Namespace Isolation**: Ensure credentials cannot be accessed across namespace boundaries
- **RBAC Integration**: Leverage Kubernetes RBAC for credential access control
- **Audit Logging**: Log authentication attempts and credential access patterns

## Chart Source Integration

### Source Type Registration

Each source type integrates with the common architecture through:

- **Type Identification**: Unique string identifier for source type detection
- **Configuration Validation**: Source-specific validation rules and constraints
- **Authentication Interface**: Standardized authentication method specification
- **Chart Resolution**: Source-specific chart loading and caching strategies

### Error Handling Patterns

Common error handling across all source types:

- **Configuration Errors**: Invalid source configuration or missing required fields
- **Authentication Failures**: Credential resolution or authentication method failures
- **Network Issues**: Transport-level failures with retry and timeout handling
- **Chart Resolution Errors**: Chart discovery, version resolution, or loading failures

## Architectural Constraints

**Protocol Compliance**: All chart source enhancements maintain compliance with the three core protocols:

- **Claiming Protocol**: Handler-specific finalizers and atomic resource discovery
- **Creation Protocol**: Status-driven progression with proper condition reporting
- **Deletion Protocol**: Coordinated cleanup through finalizer management

**Configuration Integration**: Enhanced configurations integrate with existing factory pattern without breaking configuration validation or error handling patterns.

**Operational Reliability**: Chart source features provide clear error categorization, integrate with existing Component status conditions, and support graceful recovery from transient failures.
