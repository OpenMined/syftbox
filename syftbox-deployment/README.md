# SyftBox GCP Deployment

A secure 4-pod deployment system for SyftBox on Google Cloud Platform (GCP) with cache server, data scientist VM, and bastion host access.

## 🚀 Quick Start

```bash
# Basic deployment (All 4 pods)
./deploy.sh deploy

# With Data Scientist VM public IP (no bastion needed for DS VM)
./deploy.sh deploy --with-ds-vm --ds-vm-public-ip

# Access Low Pod Jupyter (always private)
# Run this command and leave it running:
gcloud compute ssh syftbox-cluster-bastion-low \
  --zone=us-central1-a --tunnel-through-iap \
  --command="kubectl port-forward -n syftbox service/syftbox-low 8888:80 --address=0.0.0.0"

# Then in a new terminal, create the tunnel:
gcloud compute ssh syftbox-cluster-bastion-low \
  --zone=us-central1-a --tunnel-through-iap \
  -- -L 8888:localhost:8888 -N

# Then open: http://localhost:8888/jupyter/
```

## 🎯 Features

- **4-Pod Architecture**: High (Private), Low (Private), DS VM, Cache Server
- **SyftBox Integration**: Low pod and DS VM include SyftBox clients registered to cache server
- **Secure Access**: IAP-enabled bastion hosts for controlled Jupyter access
- **Flexible Deployment**: Optional public IP for DS VM only
- **Cloud-Native**: GKE cluster with auto-scaling and managed services

## 🏗️ Architecture

### 4-Pod Deployment
```
┌─────────────────────────────────────────────────────────┐
│                    GKE Cluster                          │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐   │
│  │  High Pod   │  │  Low Pod    │  │   DS VM     │   │
│  │  (Private)  │  │  (Private)  │  │   Pod       │   │
│  │             │  │             │  │             │   │
│  │ Jupyter:8889│  │ Jupyter:80  │  │ Jupyter:8888│   │
│  │ No SyftBox  │  │ + SyftBox   │  │ + SyftBox   │   │
│  │             │  │             │  │             │   │
│  └─────────────┘  └──────┬──────┘  └──────┬──────┘   │
│                          │                 │           │
│                          ↓                 ↓           │
│                   ┌─────────────────────────┐         │
│                   │   Cache Server          │         │
│                   │   (Local Only)          │         │
│                   │   + MinIO Storage       │         │
│                   └─────────────────────────┘         │
│                                                        │
└────────────────────────────────────────────────────────┘

Access Methods:
- High Pod: Bastion access only (syftbox-cluster-bastion-high)
- Low Pod: Bastion access only (syftbox-cluster-bastion-low)
- DS VM: Bastion access or optional public IP
- Cache Server: Internal only (ClusterIP)
```

### Component Details

| Component | Service Type | Port | SyftBox Client | Access Method |
|-----------|-------------|------|----------------|---------------|
| High Pod | ClusterIP | 8889 | ❌ No | Bastion (Required) |
| Low Pod | ClusterIP | 80 | ✅ Yes | Bastion (Required) |
| DS VM | ClusterIP* | 8888 | ✅ Yes | Bastion or Public IP* |
| Cache Server | ClusterIP | 8080 | N/A | Internal Only |

*DS VM can be LoadBalancer with `--ds-vm-public-ip` flag

## 📋 Prerequisites

```bash
# Install required tools
./scripts/setup-prerequisites.sh

# Configure GCP
gcloud auth login
gcloud config set project YOUR_PROJECT_ID
```

Required tools: `gcloud`, `docker`, `terraform`, `helm`, `kubectl`

## 🚀 Deployment

### 1. Configure Environment
```bash
cp .env.example .env
# Edit .env with your PROJECT_ID and settings
```

### 2. Deploy Infrastructure

```bash
# Deploy all 4 pods (High, Low, DS VM, Cache Server)
# Uses pre-built Docker images from Docker Hub
./deploy.sh deploy

# Deploy with DS VM public IP (no bastion needed for DS VM)
./deploy.sh deploy --with-ds-vm --ds-vm-public-ip

# Deploy with mock database (for testing)
./deploy.sh deploy --with-mock-db

# Build new images and deploy (optional)
./deploy.sh deploy --build-images
```

### 3. Building and Pushing Images to Docker Hub

The deployment uses pre-built images from Docker Hub (`docker.io/openmined/syftbox-test`). 

#### **Requirements for Building:**

⚠️ **Important**: Building SyftBox images requires the SyftBox source code. The build script must be run from the **SyftBox deployment directory** which should be inside the SyftBox repository.

**Directory Structure:**
```
syftbox/                          # SyftBox main repository
├── go.mod                        # Required for building
├── go.sum                        # Required for building
├── cmd/client/                   # SyftBox client source
├── syftbox-deployment/          # This deployment directory
│   ├── build-images.sh          # Build script
│   ├── deploy.sh                # Deploy script
│   └── docker/                  # Dockerfiles
└── ...
```

#### **For OpenMined Team Members:**

To build and push new images to the `openmined/syftbox-test` repository:

```bash
# 1. Ensure you're in the SyftBox deployment directory
cd syftbox/syftbox-deployment

# 2. Get Docker Hub credentials from OpenMined team
export DOCKER_USERNAME=your_openmined_username
export DOCKER_PASSWORD=your_docker_hub_token

# 3. Build and push all images
./build-images.sh

# 4. Force rebuild existing images
./build-images.sh --force

# 5. Deploy using your newly built images
./deploy.sh deploy
```

#### **For External Users:**

To build and push to your own Docker Hub repository:

```bash
# 1. Ensure you're in the SyftBox deployment directory
cd syftbox/syftbox-deployment

# 2. Set your Docker Hub credentials
export DOCKER_USERNAME=your_username
export DOCKER_PASSWORD=your_token
export DOCKER_REPOSITORY=your_username/your_repo

# 3. Build and push images
./build-images.sh

# 4. Deploy using your custom images
./deploy.sh deploy
# or
IMAGE_REGISTRY=docker.io/your_username/your_repo ./deploy.sh deploy
```

### 4. Available Docker Images

The following pre-built images are available at Docker Hub:

| Image | Purpose | Docker Hub URL |
|-------|---------|----------------|
| `syftbox-high` | High pod - private operations | `docker.io/openmined/syftbox-test/syftbox-high:latest` |
| `syftbox-low` | Low pod - web services + SyftBox | `docker.io/openmined/syftbox-test/syftbox-low:latest` |
| `syftbox-ds-vm` | Data Scientist VM + SyftBox | `docker.io/openmined/syftbox-test/syftbox-ds-vm:latest` |
| `syftbox-cache-server` | Cache server (local-only) | `docker.io/openmined/syftbox-test/syftbox-cache-server:latest` |
| `syftbox-dataowner` | Legacy data owner | `docker.io/openmined/syftbox-test/syftbox-dataowner:latest` |

You can pull these images directly:
```bash
docker pull docker.io/openmined/syftbox-test/syftbox-high:latest
docker pull docker.io/openmined/syftbox-test/syftbox-low:latest
docker pull docker.io/openmined/syftbox-test/syftbox-ds-vm:latest
docker pull docker.io/openmined/syftbox-test/syftbox-cache-server:latest
```

### 5. Verify Deployment
```bash
# Check all pods are running
kubectl get pods -n syftbox

# Expected output:
# NAME                                    READY   STATUS    RESTARTS   AGE
# syftbox-cache-server-xxx-xxx           1/1     Running   0          5m
# syftbox-ds-vm-xxx-xxx                  1/1     Running   0          5m
# syftbox-high-xxx-xxx                   1/1     Running   0          5m
# syftbox-low-xxx-xxx                    1/1     Running   0          5m
# syftbox-minio-0                        1/1     Running   0          5m
```

## 🔑 Accessing Jupyter Notebooks

All pods use **bastion access** for security, except DS VM can optionally have public IP.

### High Pod (Private - Sensitive Work)
```bash
# Terminal 1: Set up port forwarding on bastion (leave running)
gcloud compute ssh syftbox-cluster-bastion-high \
  --zone=us-central1-a --tunnel-through-iap \
  --command="kubectl port-forward -n syftbox service/syftbox-high 8889:8889 --address=0.0.0.0"

# Terminal 2: Create tunnel to bastion
gcloud compute ssh syftbox-cluster-bastion-high \
  --zone=us-central1-a --tunnel-through-iap \
  -- -L 8889:localhost:8889 -N

# Access Jupyter: http://localhost:8889/lab
```

### Low Pod (SyftBox Client)
```bash
# Terminal 1: Set up port forwarding on bastion (leave running)
gcloud compute ssh syftbox-cluster-bastion-low \
  --zone=us-central1-a --tunnel-through-iap \
  --command="kubectl port-forward -n syftbox service/syftbox-low 8888:80 --address=0.0.0.0"

# Terminal 2: Create tunnel to bastion
gcloud compute ssh syftbox-cluster-bastion-low \
  --zone=us-central1-a --tunnel-through-iap \
  -- -L 8888:localhost:8888 -N

# Access Jupyter: http://localhost:8888/jupyter/
```

### DS VM (Data Science + SyftBox)

**Option 1: Bastion Access (Default)**
```bash
# Terminal 1: Set up port forwarding on bastion (leave running)
gcloud compute ssh syftbox-cluster-bastion-ds-vm \
  --zone=us-central1-a --tunnel-through-iap \
  --command="kubectl port-forward -n syftbox service/syftbox-ds-vm 8888:8888 --address=0.0.0.0"

# Terminal 2: Create tunnel to bastion
gcloud compute ssh syftbox-cluster-bastion-ds-vm \
  --zone=us-central1-a --tunnel-through-iap \
  -- -L 8888:localhost:8888 -N

# Access Jupyter: http://localhost:8888/
```

**Option 2: Public IP (if deployed with --ds-vm-public-ip)**
```bash
# Get external IP
kubectl get service syftbox-ds-vm -n syftbox

# Access directly: http://EXTERNAL_IP:8888/
```

## 🛠️ Management Commands

### Pod Management
```bash
# Check pod status
kubectl get pods -n syftbox

# View logs
kubectl logs -l app.kubernetes.io/component=high-pod -n syftbox --tail=50
kubectl logs -l app.kubernetes.io/component=low-pod -n syftbox --tail=50
kubectl logs -l app.kubernetes.io/component=ds-vm-pod -n syftbox --tail=50

# Execute commands in pods
kubectl exec -it deploy/syftbox-high -n syftbox -- bash
kubectl exec -it deploy/syftbox-low -n syftbox -- bash
kubectl exec -it deploy/syftbox-ds-vm -n syftbox -- bash
```

### SyftBox Client Management
```bash
# Check SyftBox client status
kubectl exec -n syftbox deployment/syftbox-low -- supervisorctl status syftbox-client
kubectl exec -n syftbox deployment/syftbox-ds-vm -- supervisorctl status syftbox-client

# Restart SyftBox client
kubectl exec -n syftbox deployment/syftbox-low -- supervisorctl restart syftbox-client
kubectl exec -n syftbox deployment/syftbox-ds-vm -- supervisorctl restart syftbox-client

# View SyftBox configuration
kubectl exec -n syftbox deployment/syftbox-low -- cat /home/appuser/.syftbox/config.json
kubectl exec -n syftbox deployment/syftbox-ds-vm -- cat /home/appuser/.syftbox/config.json
```

### Cache Server Management
```bash
# Test cache server connectivity
kubectl exec -n syftbox deployment/syftbox-low -- curl -s -H 'Host: syftbox.local' http://syftbox-cache-server:8080/

# Check cache server logs
kubectl logs -l component=cache-server -n syftbox --tail=50
```

## 👥 User Management

Grant users access to bastions:

```bash
# Required roles for bastion access
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

## 🔧 Configuration

### Deployment Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--with-ds-vm` | Deploy Data Scientist VM | false |
| `--ds-vm-public-ip` | Give DS VM public IP | false |
| `--with-mock-db` | Deploy mock database | false |
| `--with-cache` | Enable cache server | true |
| `--build-images` | Build and push Docker images during deployment | false |

### Docker Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `IMAGE_REGISTRY` | Docker image registry | `docker.io/openmined/syftbox-test` |
| `DOCKER_USERNAME` | Docker Hub username | (required for building) |
| `DOCKER_PASSWORD` | Docker Hub password/token | (required for building) |
| `DOCKER_REPOSITORY` | Docker Hub repository | `openmined/syftbox-test` |

### Email Configuration
```bash
# Set custom email addresses for SyftBox clients
terraform.tfvars:
low_pod_email = "alice@company.com"
ds_vm_email = "bob@company.com"
```

## 🔍 Troubleshooting

### Common Issues

**Pod Not Starting**
```bash
# Check pod events
kubectl describe pod POD_NAME -n syftbox

# Check logs
kubectl logs POD_NAME -n syftbox
```

**Bastion Access Denied**
```bash
# Verify IAM roles
gcloud projects get-iam-policy YOUR_PROJECT_ID

# Test IAP access
gcloud compute ssh syftbox-cluster-bastion-high --zone=us-central1-a --tunnel-through-iap --dry-run
```

**SyftBox Client Issues**
```bash
# Check client status
kubectl exec -n syftbox deployment/syftbox-low -- supervisorctl status

# Restart all services
kubectl exec -n syftbox deployment/syftbox-low -- supervisorctl restart all
```

## 🧹 Cleanup

```bash
# Destroy all resources
./deploy.sh destroy
```

## 📊 Architecture Summary

### Security Features
- **Zero public IPs**: All pods use ClusterIP (internal only)
- **IAP-only access**: Bastion hosts accessible via Identity-Aware Proxy
- **Network isolation**: VPC with private subnets
- **Encrypted transit**: All traffic encrypted end-to-end

### Pod Purposes
- **High Pod**: Sensitive/private operations (no cache server access)
- **Low Pod**: Web services + SyftBox client
- **DS VM**: Data science workloads + SyftBox client
- **Cache Server**: Local SyftBox server (no internet access)

### Access Patterns
- **High Pod**: Always private (bastion required)
- **Low Pod**: Always private (bastion required)
- **DS VM**: Private by default, optional public IP
- **Cache Server**: Internal cluster only

## 📄 Support

For issues:
1. Check troubleshooting section
2. Review pod logs: `kubectl logs POD_NAME -n syftbox`
3. Verify infrastructure: `terraform plan`
4. Check deployment status: `kubectl get all -n syftbox`