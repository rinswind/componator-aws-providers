# Config-Reader Component Handler

## Overview

The config-reader handler reads ConfigMaps from arbitrary namespaces and exports selected values in Component `handlerStatus`. This enables Compositions to bootstrap from external configuration (e.g., Terraform outputs, shared infrastructure state) by referencing config-reader Components as dependencies.

## Use Cases

- **Bootstrapping from Terraform**: Read Terraform outputs stored in ConfigMaps
- **Shared Infrastructure State**: Access cluster-wide configuration managed by other systems
- **Cross-Namespace Configuration**: Read configuration from infrastructure namespaces
- **Configuration Resolution**: Provide values for template variable resolution in dependent Components

## Architecture

### Watch-Based Change Propagation

The config-reader handler implements a watch-based cascade pattern:

1. Handler watches all ConfigMaps cluster-wide
2. ConfigMap changes trigger reconciliation of affected Components
3. Deploy operation reads fresh ConfigMap values via APIReader (bypassing cache)
4. handlerStatus update triggers orchestrator reconciliation
5. Orchestrator re-resolves dependent Component templates with new values
6. Dependent Components are updated/recreated automatically
7. Changes propagate through the entire dependency chain

### Scale Considerations

- **Efficient Filtering**: Watch mapper only reconciles Components that reference the changed ConfigMap
- **Cache Bypass**: Uses APIReader for fresh reads without expanding controller cache
- **Shared ConfigMaps**: Multiple Components can reference the same ConfigMap efficiently
- **Scale Profile**: Single handler deployment serves thousands of Components

## Configuration

### Component Spec

```yaml
apiVersion: deployment-orchestrator.io/v1alpha1
kind: Component
metadata:
  name: terraform-outputs
  namespace: apps
spec:
  handler: config-reader
  config:
    sources:
      - name: terraform-outputs
        namespace: default
        exports:
          - key: eso_irsa_role_arn
          - key: vpc_id
          - key: eks_cluster_endpoint
            as: cluster_endpoint  # Optional: rename for brevity
      - name: shared-config
        namespace: kube-system
        exports:
          - key: cluster_domain
```

### Configuration Fields

**`sources`** (required, min 1): Array of ConfigMap sources to read

**`ConfigMapSource` fields:**
- **`name`** (required): ConfigMap name
- **`namespace`** (required): ConfigMap namespace  
- **`exports`** (required, min 1): Array of key export mappings

**`ExportMapping` fields:**
- **`key`** (required): ConfigMap data key to export
- **`as`** (optional): Output name (defaults to `key` if not specified)

### Handler Status

Config-reader exports a flat map of string key-value pairs:

```yaml
status:
  handlerStatus:
    eso_irsa_role_arn: "arn:aws:iam::123456789012:role/eso-role"
    vpc_id: "vpc-0123456789abcdef0"
    cluster_endpoint: "https://example.eks.amazonaws.com"
    cluster_domain: "cluster.local"
```

## Operations

### Deploy

Reads all ConfigMaps specified in config and exports values to handlerStatus.

**Behavior:**
- Synchronous operation - completes immediately
- Uses APIReader to bypass cache for fresh values
- Reads ConfigMaps sequentially from all sources
- Applies key renaming via `as` field
- Fails permanently on missing ConfigMaps or keys

**Errors:**
- ConfigMap not found → Permanent failure with detailed error
- Key not found → Permanent failure listing available keys
- Permission denied → Transient failure (RBAC may be propagating)

### CheckDeployment

Returns success immediately since Deploy completes synchronously.

**Behavior:**
- No async operations to wait for
- Always returns success

### Delete

No-op operation since config-reader creates no resources.

**Behavior:**
- Returns success immediately
- No cleanup needed

### CheckDeletion

Returns success immediately since Delete is a no-op.

**Behavior:**
- Always returns success
- No resources to wait for cleanup

## Error Handling

### Configuration Errors (Permanent)

- Invalid config JSON → Failed state
- Missing required fields → Failed state
- Invalid validation → Failed state

### Runtime Errors

**Permanent Failures:**
- ConfigMap not found → Detailed error message
- Key not found → Lists available keys to help debugging

**Transient Failures:**
- Permission denied → Requeue (RBAC may be eventually consistent)

## Example: Terraform Bootstrap

### 1. Terraform Outputs ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: terraform-outputs
  namespace: default
data:
  vpc_id: "vpc-0123456789abcdef0"
  subnet_ids: "subnet-111,subnet-222,subnet-333"
  security_group_id: "sg-0123456789abcdef0"
  eso_irsa_role_arn: "arn:aws:iam::123456789012:role/eso-role"
```

### 2. Config-Reader Component

```yaml
apiVersion: deployment-orchestrator.io/v1alpha1
kind: Component
metadata:
  name: infra-config
  namespace: apps
spec:
  handler: config-reader
  config:
    sources:
      - name: terraform-outputs
        namespace: default
        exports:
          - key: vpc_id
          - key: subnet_ids
          - key: security_group_id
          - key: eso_irsa_role_arn
```

### 3. Dependent Component Using Values

```yaml
apiVersion: deployment-orchestrator.io/v1alpha1
kind: Component
metadata:
  name: rds-instance
  namespace: apps
spec:
  handler: rds
  dependencies:
    - name: infra-config
  config:
    instanceIdentifier: myapp-db
    vpcSecurityGroupIds:
      - "{{ .dependencies.infra-config.security_group_id }}"
    # ... other RDS config using resolved values
```

### 4. Cascade on ConfigMap Update

```bash
# Update Terraform outputs
kubectl patch configmap terraform-outputs -n default \
  --type merge -p '{"data":{"vpc_id":"vpc-new"}}'

# Cascade flow:
# 1. ConfigMap watch triggers infra-config reconcile
# 2. Deploy reads new vpc_id value
# 3. handlerStatus updated with new value
# 4. Orchestrator detects drift in rds-instance
# 5. rds-instance recreated with new vpc_id
# 6. Changes propagate through dependency chain
```

## RBAC Requirements

```yaml
# ConfigMap read access (cluster-wide for cross-namespace reads)
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: config-reader-handler
rules:
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get", "list", "watch"]
```

## Testing

### Unit Tests

```bash
go test ./internal/controller/configreader/... -v
```

**Coverage:**
- Config parsing and validation
- Export mapping logic
- Error handling for missing ConfigMaps/keys
- Status serialization

### Integration Tests

Integration tests use fake clients to verify:
- ConfigMap reading and export logic
- Key renaming with `as` field
- Multiple sources and exports
- Error handling for missing resources/keys
- Status JSON serialization

## Performance Characteristics

- **Latency**: Sub-second for Deploy (direct API reads)
- **Memory**: O(exported keys) - minimal status overhead
- **CPU**: Low - synchronous reads with no computation
- **Scale**: Handles thousands of Components efficiently
- **Watch Efficiency**: Mapper filters to only affected Components

## Limitations

- **No Secret Support**: Only reads ConfigMaps (use External Secrets Operator for secrets)
- **String Values Only**: Exports ConfigMap.Data (string key-value pairs)
- **No Binary Data**: ConfigMap.BinaryData is not supported
- **No Transformation**: Values exported as-is, no computation or templating
- **Synchronous Only**: No support for dynamic/computed configuration

## Future Enhancements

Potential future capabilities (not currently planned):

- **Secret Support**: Read from Secrets with base64 decoding
- **Value Transformations**: JSON parsing, string manipulation
- **Computed Values**: Combine multiple keys or apply functions
- **Status Caching**: Avoid re-reading unchanged ConfigMaps
- **Watch Optimization**: Use predicates to filter ConfigMap changes
