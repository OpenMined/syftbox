#!/bin/bash
set -e

# Simplified SyftBox GCP Deployment Script

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TERRAFORM_DIR="$SCRIPT_DIR/terraform"
HELM_DIR="$SCRIPT_DIR/helm"
DOCKER_DIR="$SCRIPT_DIR/docker"
SCRIPTS_DIR="$SCRIPT_DIR/scripts"

# Default values
DEFAULT_REGION="us-central1"
DEFAULT_ZONE="us-central1-a"
DEFAULT_CLUSTER_NAME="syftbox-cluster"
DEFAULT_PROJECT_ID=""
DEFAULT_EMAIL="dataowner@syftbox.local"
DEFAULT_LOW_POD_EMAIL="lowpod@syftbox.local"
DEFAULT_DS_VM_EMAIL="datascientist@syftbox.local"
DEPLOY_CACHE_SERVER="false"
DEPLOY_MOCK_DATABASE="false"
DEPLOY_DS_VM="false"
DS_VM_PUBLIC_IP="false"
CONTINUE_DEPLOYMENT="false"

# Load environment variables
if [ -f "$SCRIPT_DIR/.env" ]; then
    echo "Loading environment variables from .env file..."
    source "$SCRIPT_DIR/.env"
else
    echo "No .env file found at $SCRIPT_DIR/.env"
fi

# Helper functions
print_header() {
    echo -e "\n${BLUE}==== $1 ====${NC}\n"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

print_info() {
    echo -e "${BLUE}→ $1${NC}"
}

# Generate internal GCP hostname for bastion SSH
generate_bastion_hostname() {
    local jumpbox_node_name="$1"
    local project_name="$2"
    local jumpbox_zone="$3"
    
    # Extract zone suffix (e.g., "us-central1-a" -> "c")
    local zone_suffix="${jumpbox_zone##*-}"
    
    echo "nic0.${jumpbox_node_name}.${jumpbox_zone}.${zone_suffix}.${project_name}.internal.gcpnode.com"
}

# State checking functions for continue/resume functionality
check_terraform_state() {
    if [ -f "$TERRAFORM_DIR/terraform.tfstate" ] && [ -s "$TERRAFORM_DIR/terraform.tfstate" ]; then
        cd "$TERRAFORM_DIR"
        # Check if infrastructure exists
        if terraform show -no-color 2>/dev/null | grep -q "google_container_cluster"; then
            print_success "Terraform infrastructure already deployed"
            cd "$SCRIPT_DIR"
            return 0
        fi
        cd "$SCRIPT_DIR"
    fi
    return 1
}

check_kubectl_configured() {
    if kubectl cluster-info &>/dev/null && kubectl get namespace syftbox &>/dev/null; then
        print_success "kubectl already configured for syftbox namespace"
        return 0
    fi
    return 1
}

check_syftbox_deployed() {
    if kubectl get deployment -n syftbox &>/dev/null; then
        local deployments=$(kubectl get deployment -n syftbox -o name 2>/dev/null | wc -l)
        if [ "$deployments" -gt 0 ]; then
            print_success "SyftBox already deployed ($deployments deployments found)"
            return 0
        fi
    fi
    return 1
}

check_database_initialized() {
    if kubectl get job -n syftbox -l app=database-init &>/dev/null; then
        local completed=$(kubectl get job -n syftbox -l app=database-init -o jsonpath='{.items[0].status.succeeded}' 2>/dev/null || echo "0")
        if [ "$completed" = "1" ]; then
            print_success "Database already initialized"
            return 0
        fi
    fi
    return 1
}

check_deployment_state() {
    print_header "Checking Current Deployment State"
    
    local terraform_done=false
    local kubectl_done=false
    local syftbox_done=false
    local database_done=false
    
    if check_terraform_state; then
        terraform_done=true
    else
        print_info "Terraform infrastructure not found or incomplete"
    fi
    
    if check_kubectl_configured; then
        kubectl_done=true
    else
        print_info "kubectl not configured for syftbox namespace"
    fi
    
    if check_syftbox_deployed; then
        syftbox_done=true
    else
        print_info "SyftBox not deployed yet"
    fi
    
    if check_database_initialized; then
        database_done=true
    else
        print_info "Database not initialized yet"
    fi
    
    # Return state as global variables
    export TERRAFORM_DONE=$terraform_done
    export KUBECTL_DONE=$kubectl_done
    export SYFTBOX_DONE=$syftbox_done
    export DATABASE_DONE=$database_done
    
    echo ""
    print_info "Deployment State Summary:"
    echo "  Infrastructure (Terraform): $([ "$terraform_done" = true ] && echo "✓ Complete" || echo "✗ Pending")"
    echo "  Kubernetes Config: $([ "$kubectl_done" = true ] && echo "✓ Complete" || echo "✗ Pending")"
    echo "  SyftBox Deployment: $([ "$syftbox_done" = true ] && echo "✓ Complete" || echo "✗ Pending")"
    echo "  Database Initialization: $([ "$database_done" = true ] && echo "✓ Complete" || echo "✗ Pending")"
    echo ""
}

# Check prerequisites
check_prerequisites() {
    print_header "Checking Prerequisites"
    
    local required_tools=("gcloud" "terraform" "helm" "kubectl")
    local missing_tools=()
    
    for tool in "${required_tools[@]}"; do
        if ! command -v "$tool" &> /dev/null; then
            missing_tools+=("$tool")
            print_error "$tool not found"
        else
            print_success "$tool found"
        fi
    done
    
    if [ ${#missing_tools[@]} -ne 0 ]; then
        print_error "Missing required tools: ${missing_tools[*]}"
        print_warning "Run: $SCRIPTS_DIR/setup-prerequisites.sh"
        exit 1
    fi
}

# Setup GCP project
setup_project_id() {
    print_header "Setting up GCP Project"
    
    # Get current project from gcloud
    local current_project=$(gcloud config get-value project 2>/dev/null)
    
    echo "Debug: PROJECT_ID from env = '$PROJECT_ID'"
    echo "Debug: current_project from gcloud = '$current_project'"
    
    # Trim whitespace from PROJECT_ID if it exists
    if [ -n "$PROJECT_ID" ]; then
        PROJECT_ID=$(echo "$PROJECT_ID" | xargs)
        echo "Debug: PROJECT_ID after trim = '$PROJECT_ID'"
    fi
    
    if [ -z "$PROJECT_ID" ]; then
        if [ -n "$current_project" ]; then
            PROJECT_ID="$current_project"
            print_success "Using current GCP project: $PROJECT_ID"
        else
            print_error "No GCP project configured"
            echo "Please run: gcloud config set project YOUR_PROJECT_ID"
            exit 1
        fi
    else
        print_success "Using PROJECT_ID from environment: $PROJECT_ID"
    fi
    
    # Verify project exists
    #print_info "Verifying access to project: $PROJECT_ID"
    
    # Try the command and capture both stdout and stderr
    local describe_output
    local describe_result
    
    describe_output=$(gcloud projects describe "$PROJECT_ID" 2>&1)
    describe_result=$?
    
    if [ $describe_result -ne 0 ]; then
        print_error "Project $PROJECT_ID not found or not accessible"
        echo "Debugging information:"
        echo "  Current gcloud account: $(gcloud config get-value account 2>/dev/null || echo 'Not set')"
        echo "  Current gcloud project: $(gcloud config get-value project 2>/dev/null || echo 'Not set')"
        echo "  PROJECT_ID variable: '$PROJECT_ID'"
        echo "  Command output: $describe_output"
        echo ""
        echo "Try running manually:"
        echo "  gcloud projects describe '$PROJECT_ID'"
        echo ""
        echo "Common solutions:"
        echo "  1. Make sure you're logged in: gcloud auth login"
        echo "  2. Set the project: gcloud config set project $PROJECT_ID"
        echo "  3. Check project permissions: gcloud projects get-iam-policy $PROJECT_ID"
        exit 1
    fi
    
    # Set project in gcloud
    gcloud config set project "$PROJECT_ID"
    
    export PROJECT_ID
}

# Build and push Docker images (optional - use pre-built images by default)
build_and_push_images() {
    print_header "Building and Pushing Docker Images"
    
    if [ "${BUILD_IMAGES}" == "true" ]; then
        print_info "Building and pushing new Docker images..."
        "$SCRIPT_DIR/build-images.sh" --force
    else
        print_info "Using pre-built images from Docker Hub: docker.io/openmined"
        print_info "To build new images, run: ./build-images.sh"
        print_info "To force rebuild during deploy, use: --build-images flag"
    fi
}

# Deploy infrastructure with Terraform
deploy_infrastructure() {
    print_header "Deploying Infrastructure with Terraform"
    
    cd "$TERRAFORM_DIR"
    
    # Initialize Terraform
    print_success "Initializing Terraform..."
    terraform init
    
    # Create terraform.tfvars
    cat > terraform.tfvars <<EOF
project_id = "$PROJECT_ID"
region = "${REGION:-$DEFAULT_REGION}"
zone = "${ZONE:-$DEFAULT_ZONE}"
cluster_name = "${CLUSTER_NAME:-$DEFAULT_CLUSTER_NAME}"
enable_mock_database = ${DEPLOY_MOCK_DATABASE:-false}
enable_ds_vm = ${DEPLOY_DS_VM:-false}
ds_vm_public_ip = ${DS_VM_PUBLIC_IP:-false}
low_pod_email = "${LOW_POD_EMAIL:-$DEFAULT_LOW_POD_EMAIL}"
ds_vm_email = "${DS_VM_EMAIL:-$DEFAULT_DS_VM_EMAIL}"
EOF
    
    # Plan and apply
    print_success "Planning infrastructure..."
    terraform plan
    
    print_success "Applying infrastructure..."
    terraform apply -auto-approve
    
    cd "$SCRIPT_DIR"
}

# Configure kubectl
configure_kubectl() {
    print_header "Configuring kubectl"
    
    local cluster_name="${CLUSTER_NAME:-$DEFAULT_CLUSTER_NAME}"
    local zone="${ZONE:-$DEFAULT_ZONE}"
    
    print_success "Getting cluster credentials..."
    gcloud container clusters get-credentials "$cluster_name" \
        --zone "$zone" \
        --project "$PROJECT_ID"
    
    # Create namespace
    print_success "Creating namespace..."
    kubectl create namespace syftbox --dry-run=client -o yaml | kubectl apply -f -
    
    # Set default namespace
    kubectl config set-context --current --namespace=syftbox
}

# Deploy SyftBox with Helm
deploy_syftbox() {
    print_header "Deploying SyftBox with Helm"
    
    cd "$TERRAFORM_DIR"
    
    # Get database info from Terraform outputs with fallback to direct GCP queries
    print_info "Getting database information from Terraform outputs..."
    local private_db_host=$(terraform output -raw private_database_host 2>/dev/null)
    local private_db_password=$(terraform output -raw private_database_password 2>/dev/null)
    
    if [ -z "$private_db_host" ] || [ -z "$private_db_password" ]; then
        print_warning "Could not get database information from Terraform outputs"
        print_info "Falling back to direct GCP resource queries..."
        
        # Fallback: Get database info directly from GCP and Terraform state
        local db_instance_name="${CLUSTER_NAME:-syftbox-cluster}-private-db"
        
        # Get database host directly from GCP
        print_info "Querying database instance directly from GCP..."
        local db_status=$(gcloud sql instances describe "$db_instance_name" --format="value(state)" 2>/dev/null || echo "NOT_FOUND")
        
        if [ "$db_status" = "NOT_FOUND" ]; then
            print_error "Database instance $db_instance_name not found in GCP"
            print_info "Available SQL instances:"
            gcloud sql instances list --format="table(name,state,region)" 2>/dev/null || echo "No instances found"
            print_error "Database appears to not be deployed yet. Please check Terraform deployment."
            exit 1
        elif [ "$db_status" = "PENDING_CREATE" ]; then
            print_warning "Database instance $db_instance_name is still being created (status: $db_status)"
            print_info "This usually takes 5-10 minutes. Please wait and try again."
            print_info "You can check status with: gcloud sql instances describe $db_instance_name"
            print_info "Once ready, re-run deployment with: ./deploy.sh deploy --continue [your-flags]"
            exit 1
        elif [ "$db_status" = "RUNNABLE" ]; then
            print_success "Database instance is ready, attempting to get connection details..."
            
            # Get private IP directly from GCP
            private_db_host=$(gcloud sql instances describe "$db_instance_name" --format="value(ipAddresses[0].ipAddress)" 2>/dev/null)
            
            if [ -z "$private_db_host" ]; then
                print_error "Could not get private IP address from database instance"
                print_info "Database instance details:"
                gcloud sql instances describe "$db_instance_name" --format="table(name,state,ipAddresses[].ipAddress,ipAddresses[].type)" 2>/dev/null
                exit 1
            fi
            
            # Get password from Terraform state directly
            print_info "Extracting password from Terraform state..."
            cd "$TERRAFORM_DIR"
            private_db_password=$(terraform state show 'random_password.private_db_password' 2>/dev/null | grep -E '^\s*result\s*=' | cut -d'"' -f2)
            cd "$SCRIPT_DIR"
            
            if [ -z "$private_db_password" ]; then
                print_error "Could not extract database password from Terraform state"
                print_info "Available Terraform state resources:"
                cd "$TERRAFORM_DIR"
                terraform state list | grep -E "(password|db)" || echo "No password resources found"
                cd "$SCRIPT_DIR"
                print_error "Database password not available. Please check Terraform state."
                exit 1
            fi
            
            print_success "Successfully retrieved database credentials via fallback method"
            print_info "Database Host: $private_db_host"
            print_info "Database Password: [retrieved from Terraform state]"
        else
            print_error "Database instance $db_instance_name is in unexpected state: $db_status"
            print_info "Database instance details:"
            gcloud sql instances describe "$db_instance_name" --format="table(name,state,settings.tier)" 2>/dev/null
            exit 1
        fi
    fi
    
    print_success "Database host: $private_db_host"
    print_success "Database password: [hidden]"
    
    # Show full database connection details for debugging
    print_info "Database Connection Details:"
    echo "  Host: $private_db_host"
    echo "  Port: 5432"
    echo "  Database: syftbox_private"
    echo "  Username: syftbox"
    echo "  Password: [hidden - available in Terraform state]"
    
    # Verify database instance is ready
    print_info "Verifying database instance is ready..."
    local db_instance_name="${CLUSTER_NAME:-syftbox-cluster}-private-db"
    local db_status=$(gcloud sql instances describe "$db_instance_name" --format="value(state)" 2>/dev/null || echo "NOT_FOUND")
    
    if [ "$db_status" != "RUNNABLE" ]; then
        if [ "$db_status" = "PENDING_CREATE" ]; then
            print_warning "Database instance $db_instance_name is still being created"
            print_info "This can take 5-10 minutes. Current status: $db_status"
            print_info "You can monitor with: gcloud sql instances describe $db_instance_name"
            print_info "Once ready, re-run with: ./deploy.sh deploy --continue [your-flags]"
        else
            print_error "Database instance $db_instance_name is not ready (status: $db_status)"
            print_info "Checking database instance..."
            gcloud sql instances describe "$db_instance_name" --format="table(name,state,settings.tier)" 2>/dev/null || echo "Database instance not found"
        fi
        print_error "Please wait for database to be ready before deploying SyftBox"
        exit 1
    fi
    
    print_success "Database instance is ready"
    
    # Use Docker Hub images by default
    local image_registry="${IMAGE_REGISTRY:-docker.io/openmined}"
    
    cd "$SCRIPT_DIR"
    
    # Base helm command
    local helm_cmd="helm upgrade --install syftbox \"$HELM_DIR/syftbox\" \
        --namespace syftbox \
        --create-namespace \
        --set global.imageRegistry=\"$image_registry\" \
        --set database.enabled=true \
        --set database.external=true \
        --set database.host=\"$private_db_host\" \
        --set database.password=\"$private_db_password\" \
        --set database.username=\"syftbox\" \
        --set database.name=\"syftbox_private\" \
        --set database.port=\"5432\" \
        --set highPod.enabled=true \
        --set lowPod.enabled=true \
        --set lowPod.syftbox.email=\"${LOW_POD_EMAIL:-$DEFAULT_LOW_POD_EMAIL}\" \
        --set networkPolicies.enabled=true"
    
    # Add cache server settings if enabled
    if [ "${DEPLOY_CACHE_SERVER}" == "true" ]; then
        print_info "Cache server deployment enabled"
        helm_cmd="$helm_cmd \
            --set cacheServer.enabled=true \
            --set dataOwner.enabled=false"  # dataOwner should NOT be enabled when cache server is enabled
    else
        print_info "Cache server deployment disabled"
        helm_cmd="$helm_cmd \
            --set cacheServer.enabled=false \
            --set dataOwner.enabled=false"
    fi
    
    # Add Data Scientist VM settings if enabled
    if [ "${DEPLOY_DS_VM}" == "true" ]; then
        print_info "Data Scientist VM deployment enabled"
        helm_cmd="$helm_cmd \
            --set dsVm.enabled=true \
            --set dsVm.syftbox.email=\"${DS_VM_EMAIL:-$DEFAULT_DS_VM_EMAIL}\""
        
        # Configure DS VM service type based on public IP setting
        if [ "${DS_VM_PUBLIC_IP}" == "true" ]; then
            print_info "Data Scientist VM configured with public IP (LoadBalancer)"
            helm_cmd="$helm_cmd \
                --set dsVm.service.type=LoadBalancer \
                --set dsVm.publicIp.enabled=true"
        else
            print_info "Data Scientist VM configured with bastion access (ClusterIP)"
            helm_cmd="$helm_cmd \
                --set dsVm.service.type=ClusterIP \
                --set dsVm.publicIp.enabled=false"
        fi
    else
        helm_cmd="$helm_cmd \
            --set dsVm.enabled=false"
    fi
    
    # Add mock database settings if enabled
    if [ "${DEPLOY_MOCK_DATABASE}" == "true" ]; then
        print_info "Mock database enabled"
        local mock_db_host=$(terraform output -raw mock_database_host 2>/dev/null || echo "")
        local mock_db_password=$(terraform output -raw mock_database_password 2>/dev/null || echo "")
        helm_cmd="$helm_cmd \
            --set database.mock.enabled=true \
            --set database.mock.host=\"$mock_db_host\" \
            --set database.mock.password=\"$mock_db_password\""
    else
        helm_cmd="$helm_cmd \
            --set database.mock.enabled=false"
    fi
    
    # Add wait flag
    helm_cmd="$helm_cmd --wait"
    
    # Install/upgrade Helm chart
    print_success "Installing SyftBox Helm chart..."
    eval $helm_cmd
}

# Initialize database
init_database() {
    print_header "Initializing Database"
    
    "$SCRIPTS_DIR/init-database.sh"
}

# Get access information
get_access_info() {
    print_header "Access Information"
    
    # Get LoadBalancer IPs
    local data_owner_ip=$(kubectl get svc syftbox-data-owner -n syftbox \
        -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo "pending")
    
    echo -e "${GREEN}SyftBox deployment complete!${NC}"
    echo ""
    
    # Show pod information
    echo "Deployed Pods:"
    kubectl get pods -n syftbox
    echo ""
    
    # Generate bastion hostname for internal SSH
    local bastion_name="${CLUSTER_NAME:-syftbox-cluster}-bastion"
    local bastion_zone="${ZONE:-us-central1-a}"
    local bastion_hostname=$(generate_bastion_hostname "$bastion_name" "$PROJECT_ID" "$bastion_zone")
    
    echo "High Pod (Private) - Jupyter Lab on port 8889:"
    echo "  - Local access: kubectl port-forward -n syftbox svc/syftbox-high 8889:8889"
    echo "  - Via bastion: gcloud compute ssh $bastion_name --project $PROJECT_ID --zone $bastion_zone -- -o Hostname=$bastion_hostname -L 8889:localhost:8889 -N"
    echo "  - Then open: http://localhost:8889"
    echo ""
    
    echo "Low Pod (Public) - API on port 80, Jupyter via /jupyter/:"
    echo "  - API: kubectl port-forward -n syftbox svc/syftbox-low 8080:80"
    echo "  - Jupyter: kubectl port-forward -n syftbox svc/syftbox-low 8888:8888"
    echo "  - Via bastion: gcloud compute ssh $bastion_name --project $PROJECT_ID --zone $bastion_zone -- -o Hostname=$bastion_hostname -L 8080:localhost:80 -L 8888:localhost:8888 -N"
    echo "  - Then open: http://localhost:8080 (API) or http://localhost:8888 (Jupyter)"
    echo ""
    
    if [ "${DEPLOY_CACHE_SERVER}" == "true" ]; then
        echo "Cache Server (internal): http://syftbox-cache-server.syftbox:8080"
        echo ""
    fi
    
    # Show DS VM access if enabled
    if [ "${DEPLOY_DS_VM}" == "true" ]; then
        echo "Data Scientist VM - Jupyter Lab on port 8888:"
        if [ "${DS_VM_PUBLIC_IP}" == "true" ]; then
            echo "  - Public IP access: [Check kubectl get svc syftbox-ds-vm for external IP]"
        else
            echo "  - Via bastion: gcloud compute ssh $bastion_name --project $PROJECT_ID --zone $bastion_zone -- -o Hostname=$bastion_hostname -L 8888:localhost:8888 -N"
        fi
        echo "  - Then open: http://localhost:8888"
        echo ""
    fi
    
    echo "To access pods directly:"
    echo "  kubectl exec -it deploy/syftbox-high -n syftbox -- bash"
    echo "  kubectl exec -it deploy/syftbox-low -n syftbox -- bash"
    echo ""
    echo "To connect to the database from High pod:"
    echo "  kubectl exec -it deploy/syftbox-high -n syftbox -- db-connect"
    echo ""
    echo "Bastion host access (for port forwarding):"
    echo "  gcloud compute ssh $bastion_name --project $PROJECT_ID --zone $bastion_zone -- -o Hostname=$bastion_hostname"
    echo ""
    
    # Show Terraform outputs for quick reference
    echo "Terraform Output Commands (for copy/paste):"
    echo "  Main bastion SSH: terraform output -raw bastion_iap_ssh_command"
    echo "  Main bastion tunnel: terraform output -raw bastion_iap_tunnel_command"
    echo "  High pod tunnel: terraform output -raw high_pod_jupyter_tunnel_command"
    echo "  Low pod tunnel: terraform output -raw low_pod_jupyter_tunnel_command"
    if [ "${DEPLOY_DS_VM}" == "true" ]; then
        echo "  DS VM tunnel: terraform output -raw ds_vm_jupyter_tunnel_command"
    fi
}

# Cleanup/destroy everything
cleanup() {
    print_header "Destroying SyftBox Deployment"
    
    if [ "$1" != "--force" ]; then
        echo -e "${YELLOW}This will destroy all resources!${NC}"
        read -p "Are you sure? (yes/no): " confirm
        if [ "$confirm" != "yes" ]; then
            print_warning "Cleanup cancelled"
            exit 0
        fi
    fi
    
    # Follow terraform principles: let terraform handle all infrastructure
    cd "$TERRAFORM_DIR"
    
    # Check if terraform state exists
    if [ ! -f terraform.tfstate ]; then
        print_warning "No Terraform state found - nothing to destroy"
        cd "$SCRIPT_DIR"
        return 0
    fi
    
    # Destroy Terraform infrastructure (this will handle GKE cluster and all resources)
    print_success "Destroying infrastructure with Terraform..."
    terraform destroy -auto-approve
    
    cd "$SCRIPT_DIR"
    print_success "Cleanup complete"
}

# Show status
show_status() {
    print_header "SyftBox Deployment Status"
    
    # Check Terraform state
    echo "Infrastructure:"
    cd "$TERRAFORM_DIR"
    if [ -f terraform.tfstate ]; then
        terraform show -no-color | grep -E "(google_container_cluster|google_sql_database_instance)" | head -10
    else
        print_warning "No Terraform state found"
    fi
    cd "$SCRIPT_DIR"
    
    echo ""
    echo "Kubernetes Resources:"
    kubectl get all -n syftbox 2>/dev/null || print_warning "Namespace not found"
    
    echo ""
    echo "External IPs:"
    kubectl get svc -n syftbox 2>/dev/null | grep LoadBalancer || print_warning "No LoadBalancer services"
}

# Wait for database to be ready
wait_for_database() {
    print_header "Waiting for Database to be Ready"
    
    local db_instance_name="${CLUSTER_NAME:-syftbox-cluster}-private-db"
    local max_wait=1200  # 20 minutes max
    local wait_time=0
    local check_interval=30  # Check every 30 seconds
    
    while [ $wait_time -lt $max_wait ]; do
        local db_status=$(gcloud sql instances describe "$db_instance_name" --format="value(state)" 2>/dev/null || echo "NOT_FOUND")
        
        case "$db_status" in
            "RUNNABLE")
                print_success "Database instance $db_instance_name is ready!"
                print_info "You can now continue deployment with: ./deploy.sh deploy --continue [your-flags]"
                return 0
                ;;
            "PENDING_CREATE")
                print_info "Database still creating... ($((wait_time/60))m ${wait_time}s elapsed)"
                ;;
            "NOT_FOUND")
                print_error "Database instance $db_instance_name not found"
                print_info "Please run terraform apply first"
                return 1
                ;;
            *)
                print_warning "Database in unexpected state: $db_status"
                ;;
        esac
        
        sleep $check_interval
        wait_time=$((wait_time + check_interval))
    done
    
    print_error "Timeout waiting for database to be ready after $((max_wait/60)) minutes"
    print_info "Check database status manually: gcloud sql instances describe $db_instance_name"
    return 1
}

# Debug database connection
debug_database_connection() {
    print_header "Database Connection Debug Information"
    
    local db_instance_name="${CLUSTER_NAME:-syftbox-cluster}-private-db"
    
    # 1. Check if database instance exists
    print_info "1. Checking database instance in GCP..."
    local db_status=$(gcloud sql instances describe "$db_instance_name" --format="value(state)" 2>/dev/null || echo "NOT_FOUND")
    
    if [ "$db_status" = "NOT_FOUND" ]; then
        print_error "Database instance $db_instance_name not found"
        print_info "Available SQL instances:"
        gcloud sql instances list --format="table(name,state,region)" 2>/dev/null || echo "No instances found"
        return 1
    else
        print_success "Database instance found with status: $db_status"
    fi
    
    # 2. Show database instance details
    print_info "2. Database instance details:"
    gcloud sql instances describe "$db_instance_name" --format="table(name,state,ipAddresses[].ipAddress,ipAddresses[].type,settings.tier)" 2>/dev/null
    
    # 3. Check Terraform outputs
    print_info "3. Checking Terraform outputs..."
    cd "$TERRAFORM_DIR"
    echo "Available outputs:"
    terraform output 2>/dev/null || echo "No outputs available"
    
    # 4. Try to get database info from Terraform outputs
    print_info "4. Testing Terraform output commands..."
    local tf_host=$(terraform output -raw private_database_host 2>/dev/null)
    local tf_password=$(terraform output -raw private_database_password 2>/dev/null)
    
    if [ -n "$tf_host" ]; then
        print_success "Terraform output private_database_host: $tf_host"
    else
        print_error "Terraform output private_database_host: FAILED"
    fi
    
    if [ -n "$tf_password" ]; then
        print_success "Terraform output private_database_password: [available]"
    else
        print_error "Terraform output private_database_password: FAILED"
    fi
    
    # 5. Try fallback method
    print_info "5. Testing fallback method..."
    if [ "$db_status" = "RUNNABLE" ]; then
        local gcp_host=$(gcloud sql instances describe "$db_instance_name" --format="value(ipAddresses[0].ipAddress)" 2>/dev/null)
        local state_password=$(terraform state show 'random_password.private_db_password' 2>/dev/null | grep -E '^\s*result\s*=' | cut -d'"' -f2)
        
        if [ -n "$gcp_host" ]; then
            print_success "GCP direct query host: $gcp_host"
        else
            print_error "GCP direct query host: FAILED"
        fi
        
        if [ -n "$state_password" ]; then
            print_success "Terraform state password: [available]"
        else
            print_error "Terraform state password: FAILED"
        fi
    else
        print_warning "Database not in RUNNABLE state, skipping fallback test"
    fi
    
    # 6. Show Terraform state resources
    print_info "6. Terraform state resources:"
    echo "Database-related resources:"
    terraform state list | grep -E "(database|password|sql)" || echo "No database resources found"
    
    cd "$SCRIPT_DIR"
    
    # 7. Show what credentials would be used
    print_info "7. Database credentials that would be used:"
    echo "  Instance Name: $db_instance_name"
    echo "  Database Name: syftbox_private"
    echo "  Username: syftbox"
    echo "  Port: 5432"
    echo "  Host: $([ -n "$tf_host" ] && echo "$tf_host" || echo "$gcp_host")"
    echo "  Password: $([ -n "$tf_password" ] && echo "[from terraform output]" || echo "[from terraform state]")"
    
    print_info "8. Manual commands to get credentials:"
    echo "  Get host: gcloud sql instances describe $db_instance_name --format='value(ipAddresses[0].ipAddress)'"
    echo "  Get password: terraform state show 'random_password.private_db_password' | grep result"
    echo "  List outputs: terraform output"
    echo "  Refresh outputs: terraform refresh && terraform apply"
}

# Show help
show_help() {
    cat <<EOF
SyftBox GCP Deployment Script

Usage: $0 <command> [options]

Commands:
  deploy [options]       - Deploy SyftBox infrastructure
    --with-cache         - Include cache server pods
    --with-mock-db       - Include mock database (for testing)
    --with-ds-vm         - Include Data Scientist VM pod
    --ds-vm-public-ip    - Give DS VM a public IP (no bastion needed)
    --with-all           - Include cache server, mock database, and DS VM
    --build-images       - Build and push Docker images during deployment
    --continue           - Continue failed deployment, skip completed steps
  destroy                - Destroy all resources
  status                 - Show deployment status
  wait-for-db            - Wait for database to be ready (useful after terraform apply)
  debug-db               - Debug database connection issues and show all credentials
  access-info            - Show access information for all deployed services
  help                   - Show this help message

Environment Variables:
  PROJECT_ID            - GCP project ID (required)
  EMAIL                 - Email for syftbox client (default: dataowner@syftbox.local)
  LOW_POD_EMAIL         - Email for Low pod SyftBox client (default: lowpod@syftbox.local)
  DS_VM_EMAIL           - Email for DS VM SyftBox client (default: datascientist@syftbox.local)
  REGION                - GCP region (default: us-central1)
  ZONE                  - GCP zone (default: us-central1-a)
  DEPLOY_CACHE_SERVER   - Deploy cache server (default: false)
  DEPLOY_MOCK_DATABASE  - Deploy mock database (default: false)
  DEPLOY_DS_VM          - Deploy Data Scientist VM (default: false)
  DS_VM_PUBLIC_IP       - Give DS VM public IP (default: false)
  CLUSTER_NAME          - GKE cluster name (default: syftbox-cluster)

Example:
  export PROJECT_ID=my-gcp-project
  export EMAIL=user@company.com
  $0 deploy
EOF
}

# Main script
main() {
    case "${1:-help}" in
        deploy)
            # Parse all flags
            shift  # Remove 'deploy' from arguments
            while [[ $# -gt 0 ]]; do
                case $1 in
                    --with-cache)
                        DEPLOY_CACHE_SERVER="true"
                        print_info "Cache server deployment enabled"
                        shift
                        ;;
                    --with-mock-db)
                        DEPLOY_MOCK_DATABASE="true"
                        print_info "Mock database deployment enabled"
                        shift
                        ;;
                    --with-ds-vm)
                        DEPLOY_DS_VM="true"
                        print_info "Data Scientist VM deployment enabled"
                        shift
                        ;;
                    --ds-vm-public-ip)
                        DS_VM_PUBLIC_IP="true"
                        print_info "Data Scientist VM will have public IP (no bastion)"
                        shift
                        ;;
                    --with-all)
                        DEPLOY_CACHE_SERVER="true"
                        DEPLOY_MOCK_DATABASE="true"
                        DEPLOY_DS_VM="true"
                        print_info "All components deployment enabled (cache server, mock database, DS VM)"
                        shift
                        ;;
                    --build-images)
                        BUILD_IMAGES="true"
                        print_info "Will build and push Docker images during deployment"
                        shift
                        ;;
                    --continue)
                        CONTINUE_DEPLOYMENT="true"
                        print_info "Continue mode enabled - will skip completed steps"
                        shift
                        ;;
                    *)
                        print_error "Unknown flag: $1"
                        show_help
                        exit 1
                        ;;
                esac
            done
            check_prerequisites
            setup_project_id
            
            # Check current state if continue mode is enabled
            if [ "$CONTINUE_DEPLOYMENT" = "true" ]; then
                check_deployment_state
            fi
            
            # Deploy infrastructure (skip if already done)
            if [ "$CONTINUE_DEPLOYMENT" = "true" ] && [ "$TERRAFORM_DONE" = "true" ]; then
                print_info "Skipping infrastructure deployment - already complete"
            else
                deploy_infrastructure
            fi
            
            # Build images (always optional)
            build_and_push_images
            
            # Configure kubectl (skip if already done)
            if [ "$CONTINUE_DEPLOYMENT" = "true" ] && [ "$KUBECTL_DONE" = "true" ]; then
                print_info "Skipping kubectl configuration - already complete"
            else
                configure_kubectl
            fi
            
            # Deploy SyftBox (skip if already done)
            if [ "$CONTINUE_DEPLOYMENT" = "true" ] && [ "$SYFTBOX_DONE" = "true" ]; then
                print_info "Skipping SyftBox deployment - already complete"
            else
                deploy_syftbox
            fi
            
            # Initialize database (skip if already done)
            if [ "$CONTINUE_DEPLOYMENT" = "true" ] && [ "$DATABASE_DONE" = "true" ]; then
                print_info "Skipping database initialization - already complete"
            else
                echo "skipping database initialization - this is only needed for first-time deployments"
            fi
            
            get_access_info
            ;;
        destroy)
            setup_project_id
            cleanup "$2"
            ;;
        status)
            show_status
            ;;
        wait-for-db)
            setup_project_id
            wait_for_database
            ;;
        debug-db)
            setup_project_id
            debug_database_connection
            ;;
        access-info)
            setup_project_id
            # Auto-detect deployed components
            export DEPLOY_CACHE_SERVER=$(kubectl get deployment syftbox-cache-server -n syftbox &>/dev/null && echo "true" || echo "false")
            export DEPLOY_DS_VM=$(kubectl get deployment syftbox-ds-vm -n syftbox &>/dev/null && echo "true" || echo "false")
            export DS_VM_PUBLIC_IP=$(kubectl get svc syftbox-ds-vm -n syftbox -o jsonpath='{.spec.type}' 2>/dev/null | grep -q "LoadBalancer" && echo "true" || echo "false")
            get_access_info
            ;;
        help)
            show_help
            ;;
        *)
            print_error "Unknown command: $1"
            show_help
            exit 1
            ;;
    esac
}

# Run main function
main "$@"