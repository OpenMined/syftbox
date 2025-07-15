# SyftBox GCP Deployment

A simplified one-click deployment system for SyftBox on Google Cloud Platform (GCP) with PostgreSQL database connectivity.

## 🎯 Features

- **One-click deployment**: Deploy complete infrastructure with a single command
- **Cloud SQL PostgreSQL**: Managed database with private IP access
- **GKE cluster**: Kubernetes cluster with auto-scaling nodes
- **Enhanced data owner pod**: Includes Jupyter Lab and PostgreSQL tools
- **Simple cleanup**: Destroy everything with one command

## 📋 Prerequisites

- GCP account with billing enabled
- GCP project with sufficient permissions
- Terraform authentication with GCP (see [Authentication](#-gcp-authentication))

## 🚀 Quick Start

### 1. Setup Prerequisites

```bash
# Clone or download this repository
cd syftbox-deployment

# Make scripts executable
chmod +x deploy.sh build-and-push.sh scripts/*.sh

# Install required tools (optional - script will check)
./scripts/setup-prerequisites.sh
```

### 2. Configure Environment

```bash
# Copy example environment file
cp .env.example .env

# Edit with your settings
export PROJECT_ID="your-gcp-project-id"
export REGION="us-central1"
export ZONE="us-central1-a"
```

### 3. Deploy SyftBox

```bash
# Deploy everything
./deploy.sh deploy
```

This will:
1. Build and push Docker images to GCP Artifact Registry
2. Deploy GCP infrastructure with Terraform
3. Create GKE cluster and Cloud SQL database
4. Deploy SyftBox with Helm
5. Initialize database schema
6. Display access information

### 4. Access Your Deployment

After deployment, you'll get access information:

```
Data Owner Services:
  - SyftBox: http://EXTERNAL_IP:7938
  - Jupyter Lab: http://EXTERNAL_IP:8888

To access the data owner pod:
  kubectl exec -it deploy/syftbox-data-owner -n syftbox -- bash

To connect to the database from the pod:
  db-connect
```

## 📁 Project Structure

```
syftbox-deployment/
├── README.md                   # This file
├── deploy.sh                   # Main deployment script
├── build-and-push.sh          # Docker build and push
├── .env.example               # Environment variables template
├── .gitignore                 # Git ignore rules
├── docker/
│   ├── Dockerfile.server      # Cache server image (for mock DB)
│   └── Dockerfile.dataowner   # Data owner client image (for private DB)
├── terraform/
│   ├── main.tf                # Main Terraform configuration
│   ├── variables.tf           # Input variables
│   ├── outputs.tf             # Output values
│   ├── vpc.tf                 # VPC and networking
│   ├── gke.tf                 # GKE cluster
│   ├── database.tf            # Cloud SQL database
│   └── terraform.tfvars.example # Example variables
├── helm/
│   └── syftbox/
│       ├── Chart.yaml         # Helm chart metadata
│       ├── values.yaml        # Configuration values
│       └── templates/         # Kubernetes templates
│           ├── _helpers.tpl   # Template helpers
│           ├── configmap.yaml # Configuration
│           ├── database.yaml  # Database resources
│           ├── cache-server.yaml # Cache server
│           ├── data-owner.yaml # Data owner pod
│           └── services.yaml  # Additional services
└── scripts/
    ├── setup-prerequisites.sh # Install required tools
    ├── init-database.sh      # Database initialization
    └── cleanup.sh            # Cleanup script
```

## 🛠️ Commands

### Deploy

```bash
./deploy.sh deploy    # Deploy complete infrastructure
```

### Status

```bash
./deploy.sh status    # Show deployment status
```

### Cleanup

```bash
./deploy.sh destroy   # Destroy all resources
./scripts/cleanup.sh  # Interactive cleanup options
```

## 🔧 Configuration

### Environment Variables

Create a `.env` file or export these variables:

```bash
# Required
PROJECT_ID="your-gcp-project-id"

# Optional (with defaults)
REGION="us-central1"
ZONE="us-central1-a" 
CLUSTER_NAME="syftbox-cluster"
```

### Terraform Variables

Copy `terraform/terraform.tfvars.example` to `terraform/terraform.tfvars` and customize:

```hcl
project_id = "your-gcp-project-id"
region = "us-central1"
zone = "us-central1-a"
cluster_name = "syftbox-cluster"
node_count = 3
machine_type = "e2-standard-4"
database_tier = "db-f1-micro"
```

### Helm Values

Customize `helm/syftbox/values.yaml` for:
- Resource limits and requests
- Enable/disable Jupyter Lab
- Database connection settings (private and mock databases)
- Service types and ports
- Network policies for enhanced security

#### Database Configuration

The values file now supports dual database configuration:

```yaml
database:
  # Private database - for sensitive data (data owner only)
  private:
    type: managed
    host: "private-db-host"
    database: syftbox_private
    username: syftbox
    password: "private-password"
  
  # Mock database - for non-sensitive data (cache server)
  mock:
    type: managed  
    host: "mock-db-host"
    database: syftbox_mock
    username: syftbox
    password: "mock-password"
```

## 💾 Database Access

The deployment creates **two separate PostgreSQL databases** for enhanced security:

### Database Architecture

1. **Private Database** (`syftbox_private`):
   - Contains sensitive user data and credentials
   - Accessible only by the data owner pod
   - Used for: users, datasites, personal data

2. **Mock Database** (`syftbox_mock`):
   - Contains non-sensitive, public data
   - Accessible by cache server and data owner
   - Used for: public APIs, cached data, non-sensitive metadata

### From Data Owner Pod

```bash
# Access the pod
kubectl exec -it deploy/syftbox-data-owner -n syftbox -- bash

# Connect to private database (sensitive data)
db-connect-private

# Connect to mock database (public data)  
db-connect-mock

# Legacy command (uses private database)
db-connect

# Or use environment variables directly
psql $DATABASE_URL  # Points to private database
```

### From Jupyter Lab

Open Jupyter Lab and use the provided notebook `SyftBox_Database_Tutorial.ipynb` for examples.

## 🐳 Docker Images

The system uses two main Docker images (plus base images) stored in GCP Artifact Registry:

### Deployment Images:
1. **syftbox-cache-server**: Cache server that connects to mock database
   - Handles public data and caching
   - Includes MinIO blob storage integration
   - Mirrors `../docker/docker-compose.yml` server configuration

2. **syftbox-dataowner**: Data owner client that connects to private database
   - Enhanced with Jupyter Lab and PostgreSQL tools
   - Includes Python data science libraries
   - Dual database connection helpers (private + mock)
   - Mirrors `../docker/docker-compose-client.yml` client configuration

### Base Images:
- **syftbox-server**: Base server image (built from `../docker/Dockerfile.server`)
- **syftbox-client**: Base client image (built from `../docker/Dockerfile.client`)

### Architecture:
- **Cache Server** → **Mock Database** (public data, external VM access)
- **Data Owner** → **Private Database** (sensitive data, pod-only access)
- **MinIO** → **Blob Storage** (mirrors docker-compose MinIO setup)

### Container Registry

Images are stored in GCP Artifact Registry at:
```
us-central1-docker.pkg.dev/YOUR_PROJECT_ID/syftbox/
```

#### Anonymous Pulling

To enable anonymous pulling from the Artifact Registry (for external access):

1. **Enable anonymous access to the repository**:
```bash
# Set the repository to allow anonymous reads
gcloud artifacts repositories set-iam-policy syftbox \
    --project=YOUR_PROJECT_ID \
    --location=us-central1 \
    policy.json
```

Where `policy.json` contains:
```json
{
  "bindings": [
    {
      "role": "roles/artifactregistry.reader",
      "members": [
        "allUsers"
      ]
    }
  ]
}
```

2. **Pull images without authentication**:
```bash
# Anyone can now pull images without authentication
docker pull us-central1-docker.pkg.dev/YOUR_PROJECT_ID/syftbox/syftbox-server:latest
docker pull us-central1-docker.pkg.dev/YOUR_PROJECT_ID/syftbox/syftbox-client:latest
```

**Note**: The GKE cluster automatically has access to pull from Artifact Registry in the same project without additional configuration.

## 🔐 GCP Authentication

Terraform needs to authenticate with GCP to manage resources. There are several methods:

### Method 1: Service Account Key (Recommended for CI/CD)

1. **Create a service account**:
```bash
# Set your project ID
export PROJECT_ID="your-gcp-project-id"

# Create service account
gcloud iam service-accounts create syftbox-terraform \
    --display-name="SyftBox Terraform Service Account"

# Grant necessary permissions
gcloud projects add-iam-policy-binding $PROJECT_ID \
    --member="serviceAccount:syftbox-terraform@$PROJECT_ID.iam.gserviceaccount.com" \
    --role="roles/editor"

gcloud projects add-iam-policy-binding $PROJECT_ID \
    --member="serviceAccount:syftbox-terraform@$PROJECT_ID.iam.gserviceaccount.com" \
    --role="roles/container.admin"

# Create and download key
gcloud iam service-accounts keys create ~/syftbox-terraform.json \
    --iam-account=syftbox-terraform@$PROJECT_ID.iam.gserviceaccount.com
```

2. **Configure Terraform to use the key**:
```bash
# Set environment variable
export GOOGLE_APPLICATION_CREDENTIALS=~/syftbox-terraform.json

# Or add to your .env file
echo "GOOGLE_APPLICATION_CREDENTIALS=~/syftbox-terraform.json" >> .env
```

### Method 2: Application Default Credentials (Recommended for Development)

1. **Install and configure gcloud CLI**:
```bash
# Install gcloud CLI (if not installed)
curl https://sdk.cloud.google.com | bash
exec -l $SHELL

# Login with your user account
gcloud auth login

# Set your project
gcloud config set project your-gcp-project-id

# Create application default credentials
gcloud auth application-default login
```

2. **Verify authentication**:
```bash
# Check current auth status
gcloud auth list

# Test that Terraform can authenticate
terraform init
terraform plan
```

### Method 3: Google Cloud Shell

If you're using Google Cloud Shell, authentication is automatic:

```bash
# Cloud Shell has built-in authentication
# Just set your project
gcloud config set project your-gcp-project-id

# Run deployment
./deploy.sh deploy
```

### Method 4: Service Account Impersonation

For advanced use cases where you want to impersonate a service account:

```bash
# Create the service account (as above)
# Then impersonate it
gcloud auth application-default login --impersonate-service-account=syftbox-terraform@$PROJECT_ID.iam.gserviceaccount.com

# Or set in Terraform
export GOOGLE_IMPERSONATE_SERVICE_ACCOUNT="syftbox-terraform@$PROJECT_ID.iam.gserviceaccount.com"
```

### Required GCP Permissions

The service account or user needs these permissions:

- `roles/editor` - For general resource management
- `roles/container.admin` - For GKE cluster management
- `roles/cloudsql.admin` - For Cloud SQL database management
- `roles/compute.networkAdmin` - For VPC and networking
- `roles/iam.serviceAccountUser` - For service account operations

### Troubleshooting Authentication

```bash
# Check if authentication is working
gcloud auth list
gcloud projects list

# Test Terraform authentication
terraform init
terraform plan -var="project_id=your-project-id"

# Common issues:
# 1. Wrong project ID
gcloud config get-value project

# 2. Missing permissions
gcloud projects get-iam-policy $PROJECT_ID

# 3. Quota issues
gcloud compute project-info describe --project=$PROJECT_ID
```

## 🔍 Troubleshooting

### Common Issues

**Authentication Error**
```bash
# Ensure you're authenticated with GCP
gcloud auth login
gcloud auth application-default login
```

**Docker Build Fails**
```bash
# Ensure you can access the registry
gcloud auth configure-docker us-central1-docker.pkg.dev
```

**Database Connection Issues**
```bash
# Check if database is running
kubectl get pods -n syftbox
kubectl logs deploy/syftbox-data-owner -n syftbox
```

**Terraform State Issues**
```bash
# Reset Terraform state if needed
cd terraform
rm -rf .terraform/ terraform.tfstate*
terraform init
```

### Logs and Debugging

```bash
# Check pod logs
kubectl logs -f deploy/syftbox-cache-server -n syftbox
kubectl logs -f deploy/syftbox-data-owner -n syftbox

# Check events
kubectl get events -n syftbox --sort-by='.lastTimestamp'

# Check services
kubectl get svc -n syftbox
```

## 🧹 Cleanup

### Complete Cleanup

```bash
# Interactive cleanup
./scripts/cleanup.sh

# Force cleanup (no confirmation)
./scripts/cleanup.sh force
```

### Partial Cleanup

```bash
# Clean only Kubernetes resources
./scripts/cleanup.sh kubernetes

# Clean only Docker images
./scripts/cleanup.sh docker

# Clean only Terraform infrastructure
./scripts/cleanup.sh terraform
```

## 📝 Notes

- This is a simplified deployment focused on core functionality
- No backup systems, monitoring, or complex security features
- Database uses private IP with VPC peering for security
- LoadBalancer services will create external IPs for access
- All passwords are auto-generated and stored in Terraform state

## 🤝 Support

For issues and questions:
1. Check the troubleshooting section above
2. Review deployment logs
3. Verify GCP permissions and quotas
4. Ensure all prerequisites are installed

## 📄 License

This deployment system is provided as-is for educational and development purposes.