# Simplified SyftBox One-Click GCP Deployment Plan

## 🎯 Objective
Create a simple, one-click deployment system for SyftBox on GCP with:
- Docker images pushed to ACR
- Terraform for GCP infrastructure (VPC + GKE + Cloud SQL)
- Simple Helm charts with database connectivity
- Single orchestration script

## 📁 Directory Structure

```
syftbox-deployment/
├── README.md
├── deploy.sh                          # Main deployment script
├── build-and-push.sh                  # Docker build and push
├── .env.example                       # Environment variables
├── .gitignore
├── docker/
│   └── Dockerfile.dataowner           # Enhanced data owner image
├── terraform/
│   ├── main.tf                        # Main configuration
│   ├── variables.tf                   # Variables
│   ├── outputs.tf                     # Outputs
│   ├── vpc.tf                         # VPC and networking
│   ├── gke.tf                         # GKE cluster
│   ├── database.tf                    # Cloud SQL database
│   └── terraform.tfvars.example       # Example variables
├── helm/
│   └── syftbox/
│       ├── Chart.yaml
│       ├── values.yaml                # Configuration
│       └── templates/
│           ├── _helpers.tpl           # Template helpers
│           ├── configmap.yaml         # Configuration
│           ├── database.yaml          # Database resources
│           ├── cache-server.yaml      # Cache server
│           ├── data-owner.yaml        # Data owner pod
│           └── services.yaml          # Services
└── scripts/
    ├── setup-prerequisites.sh         # Install tools
    ├── init-database.sh               # Simple DB setup
    └── cleanup.sh                     # Cleanup
```

## 🔧 Implementation Steps

### Step 1: Core Scripts

#### deploy.sh - Main orchestration
```bash
#!/bin/bash
set -e

# Commands: deploy, destroy, status, help
# Functions:
check_prerequisites()      # Check tools (az, docker, terraform, helm, kubectl, gcloud)
setup_project_id()        # Get/validate PROJECT_ID
build_and_push_images()   # Build and push to ACR
deploy_infrastructure()   # Terraform apply
configure_kubectl()       # Setup kubectl
deploy_syftbox()          # Helm install
init_database()           # Basic database setup
get_access_info()         # Show endpoints
cleanup()                 # Destroy everything
```

#### build-and-push.sh - Docker images
- Clone from `https://github.com/bitsofsteve/syftbox.git`
- Build: syftbox-server, syftbox-client, syftbox-dataowner
- Push to ACR with public access
- Data owner image includes: PostgreSQL client, Jupyter, Python data science stack

### Step 2: Terraform Infrastructure

#### main.tf
```hcl
terraform {
  required_providers {
    google = { source = "hashicorp/google", version = "~> 4.0" }
    random = { source = "hashicorp/random", version = "~> 3.1" }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

# Enable APIs
resource "google_project_service" "apis" {
  for_each = toset([
    "compute.googleapis.com",
    "container.googleapis.com",
    "sqladmin.googleapis.com",
    "servicenetworking.googleapis.com"
  ])
  service = each.value
}
```

#### variables.tf
```hcl
variable "project_id" { type = string }
variable "region" { type = string; default = "us-central1" }
variable "zone" { type = string; default = "us-central1-a" }
variable "cluster_name" { type = string; default = "syftbox-cluster" }
variable "node_count" { type = number; default = 3 }
variable "machine_type" { type = string; default = "e2-standard-4" }
variable "database_tier" { type = string; default = "db-f1-micro" }
```

#### vpc.tf - Simple networking
```hcl
# VPC
resource "google_compute_network" "vpc" {
  name = "${var.cluster_name}-vpc"
  auto_create_subnetworks = false
}

# Subnet
resource "google_compute_subnetwork" "subnet" {
  name          = "${var.cluster_name}-subnet"
  ip_cidr_range = "10.0.0.0/24"
  region        = var.region
  network       = google_compute_network.vpc.name
  
  secondary_ip_range {
    range_name    = "pods"
    ip_cidr_range = "10.1.0.0/16"
  }
  secondary_ip_range {
    range_name    = "services"
    ip_cidr_range = "10.2.0.0/16"
  }
}

# Firewall
resource "google_compute_firewall" "allow_internal" {
  name    = "${var.cluster_name}-allow-internal"
  network = google_compute_network.vpc.name
  allow { protocol = "tcp"; ports = ["0-65535"] }
  allow { protocol = "udp"; ports = ["0-65535"] }
  source_ranges = ["10.0.0.0/8"]
}

resource "google_compute_firewall" "allow_external" {
  name    = "${var.cluster_name}-allow-external"
  network = google_compute_network.vpc.name
  allow { protocol = "tcp"; ports = ["80", "443", "8080", "8888", "7938"] }
  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["syftbox"]
}

# Private IP for Cloud SQL
resource "google_compute_global_address" "private_ip_range" {
  name          = "${var.cluster_name}-private-ip"
  purpose       = "VPC_PEERING"
  address_type  = "INTERNAL"
  prefix_length = 16
  network       = google_compute_network.vpc.id
}

# VPC peering
resource "google_service_networking_connection" "private_vpc_connection" {
  network                 = google_compute_network.vpc.id
  service                 = "servicenetworking.googleapis.com"
  reserved_peering_ranges = [google_compute_global_address.private_ip_range.name]
}
```

#### gke.tf - Simple GKE cluster
```hcl
resource "google_container_cluster" "primary" {
  name     = var.cluster_name
  location = var.zone
  
  remove_default_node_pool = true
  initial_node_count       = 1
  
  network    = google_compute_network.vpc.name
  subnetwork = google_compute_subnetwork.subnet.name
  
  ip_allocation_policy {
    cluster_secondary_range_name  = "pods"
    services_secondary_range_name = "services"
  }
}

resource "google_container_node_pool" "primary_nodes" {
  name       = "${var.cluster_name}-nodes"
  location   = var.zone
  cluster    = google_container_cluster.primary.name
  node_count = var.node_count

  node_config {
    machine_type = var.machine_type
    disk_size_gb = 30
    tags         = ["syftbox"]
    oauth_scopes = ["https://www.googleapis.com/auth/cloud-platform"]
  }
}
```

#### database.tf - Simple Cloud SQL
```hcl
# Random password
resource "random_password" "db_password" {
  length  = 16
  special = true
}

# Cloud SQL instance
resource "google_sql_database_instance" "main" {
  name             = "${var.cluster_name}-db"
  database_version = "POSTGRES_15"
  region           = var.region
  deletion_protection = false

  settings {
    tier = var.database_tier
    
    ip_configuration {
      ipv4_enabled    = false
      private_network = google_compute_network.vpc.id
    }
    
    backup_configuration {
      enabled = true
      start_time = "03:00"
    }
  }
  
  depends_on = [google_service_networking_connection.private_vpc_connection]
}

# Database
resource "google_sql_database" "syftbox" {
  name     = "syftbox"
  instance = google_sql_database_instance.main.name
}

# User
resource "google_sql_user" "syftbox" {
  name     = "syftbox"
  instance = google_sql_database_instance.main.name
  password = random_password.db_password.result
}
```

#### outputs.tf
```hcl
output "cluster_endpoint" {
  value = google_container_cluster.primary.endpoint
  sensitive = true
}

output "kubectl_config" {
  value = "gcloud container clusters get-credentials ${var.cluster_name} --zone ${var.zone} --project ${var.project_id}"
}

output "database_host" {
  value = google_sql_database_instance.main.private_ip_address
}

output "database_password" {
  value = random_password.db_password.result
  sensitive = true
}

output "database_connection_name" {
  value = google_sql_database_instance.main.connection_name
}
```

### Step 3: Helm Charts

#### values.yaml - Simple configuration
```yaml
global:
  imageRegistry: syftboxregistry.azurecr.io
  namespace: syftbox

images:
  server:
    repository: syftbox-server
    tag: latest
  dataowner:
    repository: syftbox-dataowner
    tag: latest

cacheServer:
  enabled: true
  auth:
    enabled: false
  service:
    port: 8080
  resources:
    requests: { memory: "512Mi", cpu: "250m" }
    limits: { memory: "1Gi", cpu: "500m" }

dataOwner:
  enabled: true
  jupyter:
    enabled: true
  service:
    type: LoadBalancer
    ports: { syftbox: 7938, jupyter: 8888 }
  resources:
    requests: { memory: "1Gi", cpu: "500m" }
    limits: { memory: "4Gi", cpu: "2" }

database:
  type: managed  # managed or external
  host: ""       # Set by Terraform
  port: 5432
  database: syftbox
  username: syftbox
  password: ""   # Set by Terraform
```

#### _helpers.tpl - Basic helpers
```yaml
{{- define "syftbox.name" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "syftbox.fullname" -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "syftbox.labels" -}}
app.kubernetes.io/name: {{ include "syftbox.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "syftbox.databaseUrl" -}}
postgresql://{{ .Values.database.username }}:{{ .Values.database.password }}@{{ .Values.database.host }}:{{ .Values.database.port }}/{{ .Values.database.database }}
{{- end }}
```

#### cache-server.yaml - Simple server deployment
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "syftbox.fullname" . }}-cache-server
spec:
  replicas: 1
  selector:
    matchLabels: {{- include "syftbox.labels" . | nindent 6 }}
      component: cache-server
  template:
    metadata:
      labels: {{- include "syftbox.labels" . | nindent 8 }}
        component: cache-server
    spec:
      containers:
      - name: server
        image: "{{ .Values.global.imageRegistry }}/{{ .Values.images.server.repository }}:{{ .Values.images.server.tag }}"
        ports:
        - containerPort: 8080
        env:
        - name: SYFTBOX_AUTH_ENABLED
          value: "{{ .Values.cacheServer.auth.enabled | ternary "1" "0" }}"
        - name: DATABASE_URL
          value: {{ include "syftbox.databaseUrl" . }}
        resources: {{- toYaml .Values.cacheServer.resources | nindent 10 }}
---
apiVersion: v1
kind: Service
metadata:
  name: {{ include "syftbox.fullname" . }}-cache-server
spec:
  ports:
  - port: {{ .Values.cacheServer.service.port }}
    targetPort: 8080
  selector: {{- include "syftbox.labels" . | nindent 4 }}
    component: cache-server
```

#### data-owner.yaml - Data owner with database access
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "syftbox.fullname" . }}-data-owner
spec:
  replicas: 1
  selector:
    matchLabels: {{- include "syftbox.labels" . | nindent 6 }}
      component: data-owner
  template:
    metadata:
      labels: {{- include "syftbox.labels" . | nindent 8 }}
        component: data-owner
    spec:
      containers:
      - name: data-owner
        image: "{{ .Values.global.imageRegistry }}/{{ .Values.images.dataowner.repository }}:{{ .Values.images.dataowner.tag }}"
        ports:
        - containerPort: 7938
        - containerPort: 8888
        env:
        - name: DATABASE_URL
          value: {{ include "syftbox.databaseUrl" . }}
        - name: SYFTBOX_SERVER_URL
          value: "http://{{ include "syftbox.fullname" . }}-cache-server:8080"
        command: ["/bin/bash", "-c", "while true; do sleep 30; done"]
        resources: {{- toYaml .Values.dataOwner.resources | nindent 10 }}
      {{- if .Values.dataOwner.jupyter.enabled }}
      - name: jupyter
        image: "{{ .Values.global.imageRegistry }}/{{ .Values.images.dataowner.repository }}:{{ .Values.images.dataowner.tag }}"
        ports:
        - containerPort: 8888
        command: ["jupyter", "lab", "--ip=0.0.0.0", "--allow-root", "--no-browser", "--NotebookApp.token=''"]
        workingDir: /workspace
      {{- end }}
---
apiVersion: v1
kind: Service
metadata:
  name: {{ include "syftbox.fullname" . }}-data-owner
spec:
  type: {{ .Values.dataOwner.service.type }}
  ports:
  - name: syftbox
    port: {{ .Values.dataOwner.service.ports.syftbox }}
    targetPort: 7938
  - name: jupyter
    port: {{ .Values.dataOwner.service.ports.jupyter }}
    targetPort: 8888
  selector: {{- include "syftbox.labels" . | nindent 4 }}
    component: data-owner
```

### Step 4: Enhanced Data Owner Image

#### Dockerfile.dataowner - Simple but complete
```dockerfile
FROM syftboxregistry.azurecr.io/syftbox-client:latest

# Install essentials
RUN apk add --no-cache \
    postgresql-client \
    python3 py3-pip \
    curl git vim bash

# Install Python packages
RUN pip3 install --no-cache-dir \
    jupyter jupyterlab \
    pandas numpy matplotlib \
    psycopg2-binary sqlalchemy

# Configure Jupyter
RUN jupyter lab --generate-config && \
    echo "c.ServerApp.ip = '0.0.0.0'" >> /root/.jupyter/jupyter_lab_config.py && \
    echo "c.ServerApp.allow_root = True" >> /root/.jupyter/jupyter_lab_config.py && \
    echo "c.ServerApp.token = ''" >> /root/.jupyter/jupyter_lab_config.py

# Helper script for database connection
RUN cat > /usr/local/bin/db-connect << 'EOF'
#!/bin/bash
PGPASSWORD="${DB_PASSWORD}" psql -h "${DB_HOST}" -U "${DB_USER}" -d "${DB_NAME}" "$@"
EOF
RUN chmod +x /usr/local/bin/db-connect

WORKDIR /workspace
EXPOSE 7938 8888
CMD ["/bin/bash"]
```

### Step 5: Simple Database Initialization

#### scripts/init-database.sh - Basic setup
```bash
#!/bin/bash
set -e

# Get database info from Terraform
cd terraform
DB_HOST=$(terraform output -raw database_host)
DB_PASSWORD=$(terraform output -raw database_password)
cd ..

# Create basic schema
PGPASSWORD="$DB_PASSWORD" psql -h "$DB_HOST" -U syftbox -d syftbox << 'EOF'
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS datasites (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    name VARCHAR(255) NOT NULL,
    config JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_datasites_user_id ON datasites(user_id);
EOF

echo "Database initialized successfully"

# Create Kubernetes secret
kubectl create secret generic syftbox-database \
    --from-literal=host="$DB_HOST" \
    --from-literal=password="$DB_PASSWORD" \
    --from-literal=user="syftbox" \
    --from-literal=database="syftbox" \
    --namespace=syftbox \
    --dry-run=client -o yaml | kubectl apply -f -
```

## 🚀 Implementation Commands

```bash
# 1. Create structure
mkdir -p syftbox-deployment/{docker,terraform,helm/syftbox/templates,scripts}
cd syftbox-deployment

# 2. Create all files (Claude Code implements each file)

# 3. Make scripts executable
chmod +x deploy.sh build-and-push.sh scripts/*.sh

# 4. Deploy
export PROJECT_ID="your-project"
./deploy.sh deploy
```

## 📝 Key Features

✅ **Simple**: Core functionality only, no unnecessary complexity
✅ **Database**: Cloud SQL PostgreSQL with basic connectivity  
✅ **Docker**: Three images (server, client, enhanced data owner)
✅ **Networking**: Private database access via VPC peering
✅ **Tools**: Data owner pod with Jupyter + PostgreSQL client
✅ **One-click**: Single script deployment and cleanup
✅ **Cross-platform**: Helm charts work on any Kubernetes

## 🎯 For Claude Code

**"Implement this simplified SyftBox deployment system. Create all 25+ files according to the specifications. Focus on core functionality - no backup systems, monitoring, or complex security features. Just working infrastructure with database connectivity."**

**Essential components:**
- Terraform: VPC + GKE + Cloud SQL (simple setup)
- Helm: Cache server + Data owner pod with database access
- Scripts: Build images, deploy infrastructure, basic database init
- Docker: Enhanced data owner with Jupyter + PostgreSQL tools

**Create everything with full autonomy over file creation and editing.**