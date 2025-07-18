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

# Check prerequisites
check_prerequisites() {
    print_header "Checking Prerequisites"
    
    local required_tools=("gcloud" "docker" "terraform" "helm" "kubectl")
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

# Build and push Docker images
build_and_push_images() {
    print_header "Building and Pushing Docker Images"
    
    # Execute build script
    "$SCRIPT_DIR/build-and-push.sh"
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
    
    # Get database info from Terraform outputs
    local private_db_host=$(terraform output -raw private_database_host)
    local private_db_password=$(terraform output -raw private_database_password)
    local artifact_registry_url=$(terraform output -raw artifact_registry_url)
    
    cd "$SCRIPT_DIR"
    
    # Base helm command
    local helm_cmd="helm upgrade --install syftbox \"$HELM_DIR/syftbox\" \
        --namespace syftbox \
        --create-namespace \
        --set global.imageRegistry=\"$artifact_registry_url\" \
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
    
    echo "High Pod (Private) - Jupyter Lab on port 8889:"
    echo "  - Local access: kubectl port-forward -n syftbox svc/syftbox-high 8889:8889"
    echo "  - Via bastion: gcloud compute ssh syftbox-cluster-bastion --zone=us-central1-a --tunnel-through-iap -- -L 8889:localhost:8889 -N"
    echo "  - Then open: http://localhost:8889"
    echo ""
    
    echo "Low Pod (Public) - API on port 80, Jupyter via /jupyter/:"
    echo "  - API: kubectl port-forward -n syftbox svc/syftbox-low 8080:80"
    echo "  - Jupyter: kubectl port-forward -n syftbox svc/syftbox-low 8888:8888"
    echo "  - Via bastion: gcloud compute ssh syftbox-cluster-bastion --zone=us-central1-a --tunnel-through-iap -- -L 8080:localhost:80 -L 8888:localhost:8888 -N"
    echo "  - Then open: http://localhost:8080 (API) or http://localhost:8888 (Jupyter)"
    echo ""
    
    if [ "${DEPLOY_CACHE_SERVER}" == "true" ]; then
        echo "Cache Server (internal): http://syftbox-cache-server.syftbox:8080"
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
    echo "  gcloud compute ssh syftbox-cluster-bastion --zone=us-central1-a --tunnel-through-iap"
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
  destroy                - Destroy all resources
  status                 - Show deployment status
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
                    *)
                        print_error "Unknown flag: $1"
                        show_help
                        exit 1
                        ;;
                esac
            done
            check_prerequisites
            setup_project_id
            deploy_infrastructure
            build_and_push_images
            configure_kubectl
            deploy_syftbox
            init_database
            get_access_info
            ;;
        destroy)
            setup_project_id
            cleanup "$2"
            ;;
        status)
            show_status
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