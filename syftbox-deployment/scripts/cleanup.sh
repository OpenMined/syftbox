#!/bin/bash
set -e

# Cleanup Script

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TERRAFORM_DIR="$SCRIPT_DIR/../terraform"

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

# Clean Docker images
clean_docker_images() {
    print_header "Cleaning Docker Images"
    
    local images=(
        "syftboxregistry.azurecr.io/syftbox-server:latest"
        "syftboxregistry.azurecr.io/syftbox-client:latest"
        "syftboxregistry.azurecr.io/syftbox-dataowner:latest"
    )
    
    for image in "${images[@]}"; do
        if docker image inspect "$image" &>/dev/null; then
            print_info "Removing image: $image"
            docker rmi "$image" || print_warning "Failed to remove $image"
        fi
    done
    
    # Clean up dangling images
    print_info "Cleaning up dangling Docker images..."
    docker image prune -f || true
    
    print_success "Docker cleanup complete"
}

# Clean Kubernetes resources
clean_kubernetes() {
    print_header "Cleaning Kubernetes Resources"
    
    # Delete Helm release
    print_info "Deleting Helm release..."
    helm uninstall syftbox -n syftbox || print_warning "Helm release not found"
    
    # Delete namespace (this will delete all resources in it)
    print_info "Deleting namespace..."
    kubectl delete namespace syftbox --ignore-not-found
    
    # Clean up any orphaned resources
    print_info "Cleaning up any remaining resources..."
    kubectl delete all,secrets,configmaps,persistentvolumeclaims \
        -l app.kubernetes.io/name=syftbox --all-namespaces || true
    
    print_success "Kubernetes cleanup complete"
}

# Clean Terraform state
clean_terraform() {
    print_header "Cleaning Terraform Resources"
    
    cd "$TERRAFORM_DIR"
    
    if [ -f "terraform.tfstate" ]; then
        print_info "Destroying Terraform infrastructure..."
        terraform destroy -auto-approve || print_warning "Some Terraform resources may not have been destroyed"
        
        # Clean up state files
        print_info "Cleaning up Terraform state files..."
        rm -f terraform.tfstate*
        rm -f terraform.tfvars
        rm -rf .terraform/
    else
        print_warning "No Terraform state found"
    fi
    
    cd - > /dev/null
    print_success "Terraform cleanup complete"
}

# Clean temporary files
clean_temp_files() {
    print_header "Cleaning Temporary Files"
    
    # Remove any temporary build directories
    print_info "Removing temporary build directories..."
    rm -rf /tmp/syftbox-build-*
    
    # Clean up any log files
    print_info "Cleaning up log files..."
    find "$SCRIPT_DIR/.." -name "*.log" -delete 2>/dev/null || true
    
    print_success "Temporary files cleanup complete"
}

# Show current state
show_current_state() {
    print_header "Current Deployment State"
    
    echo "Docker images:"
    docker images | grep syftbox || echo "  No SyftBox images found"
    
    echo ""
    echo "Kubernetes namespaces:"
    kubectl get namespaces | grep syftbox || echo "  No SyftBox namespaces found"
    
    echo ""
    echo "Terraform state:"
    if [ -f "$TERRAFORM_DIR/terraform.tfstate" ]; then
        echo "  State file exists"
    else
        echo "  No state file found"
    fi
}

# Force cleanup (skip confirmations)
force_cleanup() {
    print_warning "Force cleanup initiated - skipping confirmations"
    
    clean_kubernetes
    clean_terraform
    clean_docker_images
    clean_temp_files
    
    print_success "Force cleanup complete"
}

# Interactive cleanup
interactive_cleanup() {
    print_header "SyftBox Deployment Cleanup"
    print_warning "This will remove all SyftBox resources!"
    
    show_current_state
    
    echo ""
    read -p "Do you want to proceed with cleanup? (yes/no): " confirm
    
    if [ "$confirm" != "yes" ]; then
        print_info "Cleanup cancelled"
        exit 0
    fi
    
    # Ask what to clean
    echo ""
    echo "What would you like to clean?"
    read -p "Clean Kubernetes resources? (y/n): " clean_k8s
    read -p "Clean Terraform infrastructure? (y/n): " clean_tf
    read -p "Clean Docker images? (y/n): " clean_docker
    read -p "Clean temporary files? (y/n): " clean_temp
    
    # Perform cleanup based on user choices
    if [ "$clean_k8s" = "y" ]; then
        clean_kubernetes
    fi
    
    if [ "$clean_tf" = "y" ]; then
        clean_terraform
    fi
    
    if [ "$clean_docker" = "y" ]; then
        clean_docker_images
    fi
    
    if [ "$clean_temp" = "y" ]; then
        clean_temp_files
    fi
    
    print_success "Cleanup complete!"
}

# Show help
show_help() {
    cat <<EOF
SyftBox Cleanup Script

Usage: $0 [command]

Commands:
  all         - Clean everything (interactive)
  force       - Force cleanup without confirmation
  docker      - Clean only Docker images
  kubernetes  - Clean only Kubernetes resources
  terraform   - Clean only Terraform resources
  temp        - Clean only temporary files
  status      - Show current deployment state
  help        - Show this help message

Examples:
  $0 all      # Interactive cleanup
  $0 force    # Clean everything without confirmation
  $0 docker   # Clean only Docker images
EOF
}

# Main function
main() {
    case "${1:-all}" in
        all)
            interactive_cleanup
            ;;
        force)
            force_cleanup
            ;;
        docker)
            clean_docker_images
            ;;
        kubernetes)
            clean_kubernetes
            ;;
        terraform)
            clean_terraform
            ;;
        temp)
            clean_temp_files
            ;;
        status)
            show_current_state
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