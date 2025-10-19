# RDS Handler

Component handler for managing AWS RDS database instances with managed passwords and automated credential management.

## Purpose

Create and manage RDS database instances with secure, AWS-managed master passwords stored in Secrets Manager. Instances are modified in place to preserve endpoints and connection strings across configuration updates.

## Configuration

### Required Fields

- **instanceID**: RDS instance identifier (must be unique within AWS region)
- **databaseEngine**: Database engine (postgres, mysql, mariadb, etc.)
- **engineVersion**: Database engine version
- **instanceClass**: RDS instance size (e.g., db.t3.micro, db.r5.large)
- **databaseName**: Initial database name to create
- **region**: AWS region for deployment
- **allocatedStorage**: Storage size in GB (minimum 20 for most engines)
- **masterUsername**: Database master username

### Managed Password Configuration

AWS RDS automatically generates secure passwords and stores them in Secrets Manager. This is enforced for security compliance.

- **manageMasterUserPassword**: Must be `true` (default, enforced)
- **masterUserSecretKmsKeyId**: Optional KMS key ARN for secret encryption

**Note**: Explicit password configuration is not supported. AWS manages password generation, storage, and optional rotation.

### Optional Fields

**Storage Configuration:**

- **storageType**: Storage type (defaults to "gp2" - General Purpose SSD)
- **storageEncrypted**: Enable storage encryption (defaults to true)
- **kmsKeyId**: KMS key ARN for storage encryption

**Network Configuration:**

- **vpcSecurityGroupIds**: List of security group IDs
- **subnetGroupName**: DB subnet group name for VPC placement
- **publiclyAccessible**: Allow public access (defaults to false)
- **port**: Database port (defaults to engine-specific port)

**Backup Configuration:**

- **backupRetentionPeriod**: Backup retention in days (defaults to 7)
- **preferredBackupWindow**: Backup window in UTC (e.g., "03:00-04:00")

**Maintenance Configuration:**

- **preferredMaintenanceWindow**: Maintenance window (e.g., "sun:04:00-sun:05:00")
- **autoMinorVersionUpgrade**: Enable automatic minor version upgrades (defaults to true)

**Performance Configuration:**

- **multiAZ**: Enable Multi-AZ deployment (defaults to false)
- **performanceInsightsEnabled**: Enable Performance Insights (defaults to false)
- **monitoringInterval**: Enhanced monitoring interval in seconds (defaults to 0/disabled)

**Protection Configuration:**

- **deletionProtection**: Prevent accidental deletion (defaults to true)
- **skipFinalSnapshot**: Skip final snapshot on deletion (defaults to false)
- **finalDBSnapshotIdentifier**: Final snapshot name (required if skipFinalSnapshot is false)

## Usage Examples

### Basic PostgreSQL Database

```yaml
apiVersion: deployments.deployment-orchestrator.io/v1alpha1
kind: Component
metadata:
  name: app-database
spec:
  handler: rds
  dependsOn:
    - vpc-config
  config:
    instanceID: "myapp-db"
    databaseEngine: "postgres"
    engineVersion: "15.4"
    instanceClass: "db.t3.micro"
    databaseName: "myapp"
    region: "us-west-2"
    allocatedStorage: 20
    masterUsername: "dbadmin"
    vpcSecurityGroupIds:
      - "{{ .vpc-config.handlerStatus.dbSecurityGroupId }}"
    subnetGroupName: "{{ .vpc-config.handlerStatus.dbSubnetGroupName }}"
```

### Production Database with Encryption and Backups

```yaml
apiVersion: deployments.deployment-orchestrator.io/v1alpha1
kind: Component
metadata:
  name: prod-database
spec:
  handler: rds
  dependsOn:
    - vpc-config
    - kms-key
  config:
    instanceID: "prod-db"
    databaseEngine: "postgres"
    engineVersion: "15.4"
    instanceClass: "db.r5.xlarge"
    databaseName: "production"
    region: "us-west-2"
    allocatedStorage: 100
    masterUsername: "dbadmin"
    
    # Security
    vpcSecurityGroupIds:
      - "{{ .vpc-config.handlerStatus.dbSecurityGroupId }}"
    subnetGroupName: "{{ .vpc-config.handlerStatus.dbSubnetGroupName }}"
    storageEncrypted: true
    kmsKeyId: "{{ .kms-key.handlerStatus.keyArn }}"
    masterUserSecretKmsKeyId: "{{ .kms-key.handlerStatus.keyArn }}"
    
    # High Availability
    multiAZ: true
    
    # Backups
    backupRetentionPeriod: 30
    preferredBackupWindow: "03:00-04:00"
    
    # Maintenance
    preferredMaintenanceWindow: "sun:04:00-sun:05:00"
    autoMinorVersionUpgrade: true
    
    # Monitoring
    performanceInsightsEnabled: true
    monitoringInterval: 60
```

### MySQL Database with Custom Configuration

```yaml
apiVersion: deployments.deployment-orchestrator.io/v1alpha1
kind: Component
metadata:
  name: mysql-database
spec:
  handler: rds
  config:
    instanceID: "mysql-db"
    databaseEngine: "mysql"
    engineVersion: "8.0.35"
    instanceClass: "db.t3.small"
    databaseName: "wordpress"
    region: "us-east-1"
    allocatedStorage: 50
    masterUsername: "admin"
    port: 3306
    
    # Storage
    storageType: "gp3"
    storageEncrypted: true
    
    # Network
    publiclyAccessible: false
    
    # Backups
    backupRetentionPeriod: 14
    skipFinalSnapshot: false
    finalDBSnapshotIdentifier: "mysql-db-final-snapshot"
```

### Accessing Database Credentials with External Secrets Operator

```yaml
apiVersion: deployments.deployment-orchestrator.io/v1alpha1
kind: Component
metadata:
  name: db-secret-sync
spec:
  handler: manifest
  dependsOn:
    - app-database
    - eso-role
  config:
    manifests:
      - |
        apiVersion: external-secrets.io/v1beta1
        kind: SecretStore
        metadata:
          name: aws-secrets
          namespace: default
        spec:
          provider:
            aws:
              service: SecretsManager
              region: us-west-2
              auth:
                jwt:
                  serviceAccountRef:
                    name: external-secrets
      - |
        apiVersion: external-secrets.io/v1beta1
        kind: ExternalSecret
        metadata:
          name: database-credentials
          namespace: default
        spec:
          refreshInterval: 1h
          secretStoreRef:
            name: aws-secrets
            kind: SecretStore
          target:
            name: database-credentials
            creationPolicy: Owner
          dataFrom:
            - extract:
                key: "{{ .app-database.handlerStatus.masterUserSecretArn }}"
```

The External Secrets Operator syncs the RDS password from Secrets Manager into a Kubernetes Secret.

## Handler Status

The handler reports database information in `status.handlerStatus`:

- **instanceStatus**: Current RDS instance status (creating, available, modifying, failed, etc.)
- **instanceARN**: AWS ARN for the RDS instance
- **endpoint**: Database connection endpoint hostname
- **port**: Database connection port number
- **availabilityZone**: AWS availability zone where instance is deployed
- **masterUserSecretArn**: AWS Secrets Manager ARN containing the master password

## Database Updates

- Instances are modified in place via AWS RDS versioning (endpoint remains constant)
- Configuration changes are applied immediately (ApplyImmediately=true)
- Some changes may require instance restart (controlled by AWS RDS)
- Major version upgrades require explicit engineVersion changes

## AWS Permissions Required

Minimum required IAM actions:

```json
[
    "rds:CreateDBInstance",
    "rds:DescribeDBInstances",
    "rds:ModifyDBInstance",
    "rds:DeleteDBInstance",
    "rds:AddTagsToResource",
    "rds:ListTagsForResource"
]
```

Additional permissions for KMS-encrypted instances:

```json
[
    "kms:CreateGrant",
    "kms:DescribeKey"
]
```
