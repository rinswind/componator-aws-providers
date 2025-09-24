# Component Samples for Helm Handler

This directory contains sample Component CRDs that demonstrate different Helm deployment scenarios using the Helm handler.

## Prerequisites

1. Kubernetes cluster with deployment-operator CRDs installed
2. deployment-handlers controller running with Helm handler enabled
3. Appropriate RBAC permissions for the controller

## Available Samples

### Basic Examples

#### `component_nginx_basic.yaml`
- **Purpose**: Simple nginx web server deployment
- **Features**: Basic nginx with minimal configuration
- **Use Case**: Quick testing and validation of Helm handler functionality

```bash
kubectl apply -f component_nginx_basic.yaml
```

#### `component_nginx_advanced.yaml`
- **Purpose**: Advanced nginx deployment with production features
- **Features**: 
  - Horizontal Pod Autoscaler (HPA)
  - Ingress with TLS
  - Resource limits and requests
  - Security contexts
  - Load balancer service
- **Use Case**: Production-ready web server deployment

```bash
kubectl apply -f component_nginx_advanced.yaml
```

### Database Examples

#### `component_postgresql.yaml`
- **Purpose**: PostgreSQL database deployment
- **Features**:
  - Custom database and user configuration
  - Persistent storage
  - Performance tuning via extended configuration
  - Metrics and monitoring integration
  - Network policies for security
- **Use Case**: Application database backend

```bash
kubectl apply -f component_postgresql.yaml
```

#### `component_redis_cache.yaml`
- **Purpose**: Redis cache deployment in dedicated namespace
- **Features**:
  - Deployed to `cache-system` namespace (demonstrates namespace targeting)
  - Authentication enabled
  - Persistent storage
  - Metrics and service monitor
  - Network policies
  - Security contexts
- **Use Case**: Application caching layer

```bash
kubectl apply -f component_redis_cache.yaml
```

### Application Examples

#### `component_wordpress.yaml`
- **Purpose**: Complete WordPress CMS with MariaDB
- **Features**:
  - WordPress with custom admin user
  - Integrated MariaDB database
  - Persistent storage for both WordPress and database
  - Ingress with TLS
  - Load balancer service
  - Resource management
- **Use Case**: Content management system deployment

```bash
kubectl apply -f component_wordpress.yaml
```

### Monitoring Examples

#### `component_prometheus.yaml`
- **Purpose**: Complete Prometheus monitoring stack
- **Features**:
  - Prometheus server with persistent storage
  - Grafana dashboard with persistent storage
  - Alertmanager with storage
  - Ingress configuration for all components
  - Uses prometheus-community Helm repository
  - Deployed to `monitoring` namespace
- **Use Case**: Cluster and application monitoring

```bash
kubectl apply -f component_prometheus.yaml
```

## Usage Instructions

### 1. Deploy the deployment-operator CRDs
First, ensure the deployment-operator CRDs are installed in your cluster:

```bash
# From the deployment-operator project
cd ../deployment-operator
make install
```

### 2. Run the deployment-handlers controller
Start the Helm handler controller:

```bash
# From the deployment-handlers project  
make run
```

### 3. Apply sample Components
Choose and apply any of the sample Components:

```bash
# Apply a basic example
kubectl apply -f config/samples/component_nginx_basic.yaml

# Check Component status
kubectl get components
kubectl describe component nginx-basic

# Check generated Helm releases
helm list -A
```

### 4. Monitor deployment progress
Watch the Component status and controller logs:

```bash
# Watch Component status
kubectl get components -w

# Check Component events
kubectl describe component <component-name>

# View controller logs (if running with make run)
# Logs will appear in the terminal where you ran 'make run'
```

### 5. Cleanup
Remove Components when done:

```bash
kubectl delete component nginx-basic
# Or delete all sample components
kubectl delete components --all
```

## Configuration Notes

### Repository Configuration
All samples use well-known public Helm repositories:
- **bitnami**: `https://repo.broadcom.com/bitnami-files`
- **prometheus-community**: `https://prometheus-community.github.io/helm-charts`

### Namespace Targeting
Some samples demonstrate namespace targeting:
- `component_redis_cache.yaml` deploys to `cache-system` namespace
- `component_prometheus.yaml` deploys to `monitoring` namespace

The handler will create these namespaces if they don't exist.

### Values Configuration
The samples demonstrate various Helm values patterns:
- **String values**: `wordpressUsername: admin`
- **Numeric values**: `replicaCount: 3`
- **Boolean values**: `enabled: true`
- **Object values**: `resources: { limits: {...} }`
- **Array values**: `drop: [ALL]`

All JSON value types are supported thanks to the `map[string]any` Values field.

### Security Considerations
Production samples include security best practices:
- Resource limits and requests
- Security contexts with non-root users
- Network policies
- Read-only root filesystems
- Capability dropping

## Troubleshooting

### Common Issues

1. **Component stuck in Pending**: Check controller logs for Helm repository access issues
2. **Failed status**: Review the Component's status conditions for specific error messages
3. **Resource conflicts**: Ensure Component names and release names don't conflict
4. **Permission errors**: Verify RBAC permissions for the controller service account

### Debugging Commands

```bash
# Check Component status
kubectl get components -o wide

# Get detailed Component information
kubectl describe component <component-name>

# Check Helm releases
helm list -A

# View Helm release details
helm status <release-name> -n <namespace>

# Check controller logs (if running in cluster)
kubectl logs -f deployment/deployment-handlers-controller-manager -n deployment-handlers-system
```

## Customization

Feel free to modify these samples for your specific use cases:

1. **Change chart versions**: Update `chart.version` to use different versions
2. **Modify values**: Adjust the `values` section to match your requirements  
3. **Add custom repositories**: Update `repository.url` to use private or different public repositories
4. **Namespace targeting**: Add or modify `namespace` in the config to deploy to specific namespaces
5. **Resource scaling**: Adjust `replicaCount`, `resources`, and `autoscaling` parameters

## Contributing

When adding new samples:
1. Follow the existing naming convention: `component_<app>_<variant>.yaml`
2. Include comprehensive comments in the values section
3. Demonstrate different Helm handler features
4. Test samples before committing
5. Update this README with sample documentation
