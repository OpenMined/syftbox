# SyftBox GCP Deployment

A secure deployment system for SyftBox on Google Cloud Platform (GCP) with PostgreSQL database connectivity and bastion host access.

## 🎯 Features

- **Secure architecture**: Bastion host for controlled access to cluster services
- **Cloud SQL PostgreSQL**: Managed database with private IP access
- **GKE cluster**: Kubernetes cluster with auto-scaling nodes
- **Enhanced data owner pod**: Includes Jupyter Lab and PostgreSQL tools
- **Internal-only services**: All cluster services use ClusterIP (no external exposure)
- **Simple cleanup**: Destroy everything with one command

## 🏗️ Architecture Overview

The deployment consists of:
- **GKE Cluster**: Kubernetes cluster running SyftBox components
- **Cloud SQL**: PostgreSQL databases for private and mock data
- **Artifact Registry**: Container image storage
- **MinIO**: Object storage for blobs
- **Bastion Host**: Secure access gateway for cluster services

## 🔐 Security Architecture

### Zero-Trust Network Model
- **No External IPs**: Bastion has no public IP address
- **IAP-Only Access**: All bastion access via Google Cloud Identity-Aware Proxy
- **Internal Services**: SyftBox, Jupyter, databases are internal-only (ClusterIP)
- **Network Isolation**: VPC with private subnets and controlled routing

### Access Flow
```
User → Google Auth → IAP → Bastion (internal) → kubectl port-forward → Jupyter
```

### Security Benefits
1. **Authentication**: Google SSO + IAM roles required
2. **Authorization**: Fine-grained IAM permissions per user
3. **Audit Trail**: All access logged in Cloud Audit Logs
4. **Zero Attack Surface**: No public IPs or open ports
5. **Encrypted Transit**: All traffic encrypted end-to-end

### Security Improvements (Future)
- **Private GKE Cluster**: Currently uses public cluster endpoint
- **Workload Identity**: Pod-level IAM authentication
- **Binary Authorization**: Signed container image validation
- **Network Policies**: Pod-to-pod traffic restrictions
- **VPC-native Networking**: Enhanced network security

## 📋 Prerequisites

### Required Tools
- `gcloud` CLI
- `docker`
- `terraform`
- `helm`
- `kubectl`

Install all prerequisites:
```bash
./scripts/setup-prerequisites.sh
```

### GCP Setup
1. Create or select a GCP project
2. Enable required APIs
3. Configure authentication:
   ```bash
   gcloud auth login
   gcloud config set project YOUR_PROJECT_ID
   ```

## 🚀 Deployment

### 1. Configure Environment
Create a `.env` file:
```bash
cp .env.example .env
# Edit .env with your configuration
```

Required variables:
- `PROJECT_ID`: Your GCP project ID
- `REGION`: GCP region (default: us-central1)
- `ZONE`: GCP zone (default: us-central1-a)
- `CLUSTER_NAME`: GKE cluster name (default: syftbox-cluster)

### 2. Configure Bastion Access

**Option A: OS Login (Recommended)**
Use Google Cloud OS Login for dynamic user management:

```bash
# No SSH keys needed in terraform.tfvars
# Users are managed through Google Cloud IAM
```

**Note:** With IAP-only access, user management is handled entirely through Google Cloud IAM - no SSH key configuration needed.

### 3. Deploy Infrastructure
```bash
./deploy.sh deploy
```

This will:
1. Deploy GCP infrastructure (VPC, GKE, Cloud SQL, Bastion)
2. Build and push Docker images
3. Deploy SyftBox with Helm
4. Initialize databases
5. Display access information

## 🔑 Accessing SyftBox Services

### Connect to Bastion Host

The bastion host uses Google Cloud IAP (Identity-Aware Proxy) for secure access without exposing any external IP addresses.

```bash
# Get the IAP SSH command from Terraform output
terraform output bastion_iap_ssh_command

# Connect to bastion via IAP (requires gcloud auth and IAM permissions)
gcloud compute ssh syftbox-cluster-bastion --zone=us-central1-a --project=YOUR_PROJECT_ID --tunnel-through-iap
```

### Access Jupyter Lab

**Option 1: Single Command IAP Tunnel (Recommended)**
```bash
# Get the tunnel command from Terraform output
terraform output bastion_iap_tunnel_command

# Create IAP tunnel with port forwarding in one command
gcloud compute ssh syftbox-cluster-bastion \
  --zone=us-central1-a \
  --project=YOUR_PROJECT_ID \
  --tunnel-through-iap \
  -- -L 8888:localhost:8888 -N
```

**Option 2: Two-Step Process**
First, SSH to bastion:
```bash
gcloud compute ssh syftbox-cluster-bastion --zone=us-central1-a --project=YOUR_PROJECT_ID --tunnel-through-iap
```

Then from the bastion host:
```bash
# Forward Jupyter port
kubectl port-forward svc/syftbox-data-owner 8888:8888 -n syftbox &
```

In a new terminal, create the IAP tunnel:
```bash
gcloud compute ssh syftbox-cluster-bastion \
  --zone=us-central1-a \
  --project=YOUR_PROJECT_ID \
  --tunnel-through-iap \
  -- -L 8888:localhost:8888 -N
```

Access Jupyter at: http://localhost:8888

### Access SyftBox Application

**Single Command IAP Tunnel:**
```bash
# Create IAP tunnel with SyftBox port forwarding
gcloud compute ssh syftbox-cluster-bastion \
  --zone=us-central1-a \
  --project=YOUR_PROJECT_ID \
  --tunnel-through-iap \
  -- -L 7938:localhost:7938 -N
```

Access SyftBox at: http://localhost:7938

### Direct Pod Access
From the bastion host:
```bash
# Access data owner pod directly
kubectl exec -it deploy/syftbox-data-owner -n syftbox -- bash

# Access cache server pod
kubectl exec -it deploy/syftbox-cache-server -n syftbox -- bash
```

## 👥 User Management

Grant users access with appropriate IAM roles:

```bash
# Essential roles for bastion access
gcloud projects add-iam-policy-binding YOUR_PROJECT_ID \
    --member="user:user@company.com" \
    --role="roles/compute.osLogin"

gcloud projects add-iam-policy-binding YOUR_PROJECT_ID \
    --member="user:user@company.com" \
    --role="roles/iap.tunnelResourceAccessor"

# Optional: Grant Kubernetes access
gcloud projects add-iam-policy-binding YOUR_PROJECT_ID \
    --member="user:user@company.com" \
    --role="roles/container.developer"
```

**Revoke Access:**
```bash
gcloud projects remove-iam-policy-binding YOUR_PROJECT_ID \
    --member="user:user@company.com" \
    --role="roles/compute.osLogin"
```


## 🛠️ Management Commands

### Cluster Management
```bash
# Check deployment status
./deploy.sh status

# View cluster resources
kubectl get all -n syftbox

# View logs
kubectl logs -l app=syftbox-data-owner -n syftbox
kubectl logs -l app=syftbox-cache-server -n syftbox
```

### Database Management
```bash
# Connect to databases from data owner pod
kubectl exec -it deploy/syftbox-data-owner -n syftbox -- db-connect
```

### Updating Deployment
```bash
# Update Helm deployment
helm upgrade syftbox helm/syftbox --namespace syftbox

# Update infrastructure
terraform plan
terraform apply
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

# Bastion configuration (optional - only if not using OS Login)
# bastion_ssh_keys = "username:ssh-rsa AAAAB3NzaC1yc2EAAAA..."
```

### Helm Values

Customize `helm/syftbox/values.yaml` for:
- Resource limits and requests
- Enable/disable Jupyter Lab
- Database connection settings (private and mock databases)
- Service types and ports (now ClusterIP for security)

## 🔍 Troubleshooting

### Common Issues

**1. Bastion IAP Access Denied**
- Verify you have the `roles/iap.tunnelResourceAccessor` IAM role
- Ensure you're authenticated with gcloud: `gcloud auth login`
- Check that the bastion host has been deployed successfully
- Verify IAP API is enabled: `gcloud services enable iap.googleapis.com`

**2. kubectl Access from Bastion**
- The bastion host is pre-configured with kubectl access
- If issues persist, run: `gcloud container clusters get-credentials CLUSTER_NAME --zone ZONE`

**3. Port Forwarding Issues**
- Ensure the service exists: `kubectl get svc -n syftbox`
- Check pod status: `kubectl get pods -n syftbox`
- Verify network connectivity from bastion to cluster

**4. Database Connection Issues**
- Check Cloud SQL instances: `gcloud sql instances list`
- Verify VPC peering: `gcloud compute networks peerings list`
- Review database logs in GCP Console

### Logs and Monitoring

```bash
# Application logs
kubectl logs -l app=syftbox-data-owner -n syftbox --tail=100
kubectl logs -l app=syftbox-cache-server -n syftbox --tail=100

# MinIO logs
kubectl logs -l app=syftbox-minio -n syftbox --tail=100

# Cluster events
kubectl get events -n syftbox --sort-by=.metadata.creationTimestamp
```

## 🔒 Security Considerations

### Network Security
- All cluster services are internal-only (no external LoadBalancer)
- Bastion host is the only external access point
- VPC firewall rules restrict access to authorized IPs only
- Private Cloud SQL instances with VPC peering

### Access Control
- SSH key-based authentication for bastion access
- Service account with minimal required permissions
- Regular security updates on bastion host
- Network segmentation between components

### Best Practices
1. **Minimal IAM Permissions**: Grant only required roles to users
2. **Regular Updates**: Keep bastion host and cluster updated
3. **Monitoring**: Enable Cloud Audit Logs for access monitoring
4. **Backup Strategy**: Regular backup of cluster state and databases

## 🧹 Cleanup

To destroy all resources:
```bash
./deploy.sh destroy
```

## 🚀 Future Security Enhancements

### TODO: Option 1 - Ingress with Authentication (Recommended Upgrade)

For improved user experience and enterprise security, consider upgrading to:

**Components to Add:**
- **Nginx Ingress Controller**: External HTTP/HTTPS routing
- **OAuth2 Proxy**: Authentication middleware
- **Cert-Manager**: Automatic SSL certificate management
- **Identity Provider Integration**: Google Workspace, Azure AD, etc.

**Benefits:**
- Direct browser access (no SSH tunneling required)
- Centralized authentication management
- Automatic SSL/TLS certificates
- Better user experience for data owners
- Audit logging and session management

**Implementation Plan:**
1. Deploy Nginx Ingress Controller
2. Configure OAuth2 proxy with identity provider
3. Set up Cert-Manager for SSL certificates
4. Create ingress rules for Jupyter access
5. Configure DNS for custom domains
6. Implement network policies for additional security

**Example Access Flow:**
```
User → https://jupyter.syftbox.company.com 
     → OAuth2 Authentication 
     → Nginx Ingress 
     → Jupyter Service (ClusterIP)
```

This upgrade maintains security while providing a more user-friendly access method.

## 📊 Architecture Diagrams

### Current Architecture (Bastion-based)
```
Internet
    ↓
Bastion Host (SSH)
    ↓
GKE Cluster (private)
├── SyftBox Data Owner (ClusterIP)
├── Cache Server (ClusterIP)
├── MinIO (ClusterIP)
└── Cloud SQL (private)
```

### Future Architecture (Ingress-based)
```
Internet
    ↓
Nginx Ingress + OAuth2
    ↓
GKE Cluster (private)
├── SyftBox Data Owner (ClusterIP)
├── Cache Server (ClusterIP)
├── MinIO (ClusterIP)
└── Cloud SQL (private)
```

## 🐳 Docker Images

The system uses two main Docker images stored in GCP Artifact Registry:

### Deployment Images:
1. **syftbox-cache-server**: Cache server that connects to mock database
2. **syftbox-dataowner**: Data owner client that connects to private database

### Architecture:
- **Cache Server** → **Mock Database** (public data)
- **Data Owner** → **Private Database** (sensitive data)
- **MinIO** → **Blob Storage**

## 📁 Project Structure

```
syftbox-deployment/
├── README.md                   # This file
├── deploy.sh                   # Main deployment script
├── build-and-push.sh          # Docker build and push
├── .env.example               # Environment variables template
├── terraform/
│   ├── main.tf                # Main Terraform configuration
│   ├── variables.tf           # Input variables
│   ├── bastion.tf             # Bastion host configuration
│   ├── vpc.tf                 # VPC and networking
│   ├── gke.tf                 # GKE cluster
│   └── database.tf            # Cloud SQL database
├── helm/
│   └── syftbox/               # Helm chart
└── scripts/
    └── setup-prerequisites.sh # Install required tools
```

## 🤝 Support

For issues and questions:
1. Check the troubleshooting section above
2. Review cluster logs and events
3. Verify infrastructure state with `terraform plan`
4. Contact the SyftBox team for application-specific issues

## 📄 License

This deployment system is provided as-is for educational and development purposes.