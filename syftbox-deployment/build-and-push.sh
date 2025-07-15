#!/bin/bash
set -e

# Build and Push Docker Images Script

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DOCKER_DIR="$SCRIPT_DIR/docker"
TERRAFORM_DIR="$SCRIPT_DIR/terraform"
# Build context is the SyftBox repo root (parent of deployment directory)
BUILD_CONTEXT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Get Artifact Registry URL from Terraform outputs if available
if [ -f "$TERRAFORM_DIR/terraform.tfstate" ]; then
    ARTIFACT_REGISTRY_URL=$(cd "$TERRAFORM_DIR" && terraform output -raw artifact_registry_url 2>/dev/null || echo "")
fi

REGISTRY="${ARTIFACT_REGISTRY_URL:-us-central1-docker.pkg.dev/${PROJECT_ID}/syftbox}"

# Helper functions
print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${BLUE}→ $1${NC}"
}

# Cleanup function
cleanup() {
    # Clean up any temporary files created during build
    if [ -f "$BUILD_CONTEXT/requirements.txt" ]; then
        rm -f "$BUILD_CONTEXT/requirements.txt"
    fi
}

# Set trap for cleanup
trap cleanup EXIT

# Prepare build context
prepare_build_context() {
    print_info "Preparing build context..."
    
    # Validate that we have the SyftBox repository structure
    if [ ! -f "$BUILD_CONTEXT/go.mod" ] || [ ! -f "$BUILD_CONTEXT/go.sum" ]; then
        print_error "SyftBox repository structure not found at: $BUILD_CONTEXT"
        print_error "Expected to find go.mod and go.sum in the parent directory of the deployment."
        exit 1
    fi
    
    print_success "Build context ready at: $BUILD_CONTEXT"
}

# Build deployment images directly from source
build_deployment_images() {
    print_info "Building deployment images from source..."
    
    # Copy requirements file into build context
    cp "$DOCKER_DIR/requirements.txt" "$BUILD_CONTEXT/requirements.txt"
    
    # Build cache server image (from syftbox source)
    print_info "Building syftbox-cache-server..."
    docker build -t "$REGISTRY/syftbox-cache-server:latest" \
        -f "$DOCKER_DIR/Dockerfile.server" "$BUILD_CONTEXT"
    
    # Build data owner client image (from syftbox source)
    print_info "Building syftbox-dataowner..."
    docker build -t "$REGISTRY/syftbox-dataowner:latest" \
        -f "$DOCKER_DIR/Dockerfile.dataowner" "$BUILD_CONTEXT"
}

# Login to Artifact Registry
login_to_registry() {
    print_info "Logging in to GCP Artifact Registry..."
    
    # Check if PROJECT_ID is set
    if [ -z "$PROJECT_ID" ]; then
        print_error "PROJECT_ID environment variable is not set"
        exit 1
    fi
    
    print_info "Current gcloud account: $(gcloud config get-value account 2>/dev/null || echo 'not set')"
    print_info "Current gcloud project: $(gcloud config get-value project 2>/dev/null || echo 'not set')"
    
    # Check if already configured
    if grep -q "us-central1-docker.pkg.dev" ~/.docker/config.json 2>/dev/null; then
        print_info "Docker already configured for Artifact Registry"
    else
        # Configure Docker to use gcloud as credential helper (with explicit yes to avoid prompt)
        print_info "Configuring Docker for Artifact Registry..."
        if ! yes | gcloud auth configure-docker us-central1-docker.pkg.dev; then
            print_error "Failed to configure Docker for Artifact Registry. Please ensure:"
            echo "  1. You are logged in to GCP: gcloud auth login"
            echo "  2. You have application default credentials: gcloud auth application-default login"
            echo "  3. You have push permissions to the Artifact Registry"
            exit 1
        fi
    fi
    
    print_success "Docker configured for Artifact Registry"
}

# Push images
push_images() {
    print_info "Pushing images to registry..."
    
    local images=(
        "$REGISTRY/syftbox-cache-server:latest"
        "$REGISTRY/syftbox-dataowner:latest"
    )
    
    for image in "${images[@]}"; do
        print_info "Pushing $image..."
        docker push "$image"
        print_success "Pushed $image"
    done
}

# Verify images
verify_images() {
    print_info "Verifying images..."
    
    local images=(
        "$REGISTRY/syftbox-cache-server:latest"
        "$REGISTRY/syftbox-dataowner:latest"
    )
    
    for image in "${images[@]}"; do
        if docker image inspect "$image" &>/dev/null; then
            print_success "Image $image exists locally"
        else
            print_error "Image $image not found"
            exit 1
        fi
    done
}

# Main function
main() {
    print_info "Starting Docker build and push process..."
    
    # Prepare build context and build deployment images
    prepare_build_context
    build_deployment_images
    
    # Verify all images
    verify_images
    
    # Login and push
    login_to_registry
    push_images
    
    print_success "All images built and pushed successfully!"
    echo ""
    echo "Images available at:"
    echo "  - $REGISTRY/syftbox-cache-server:latest (cache server - no DB dependencies)"
    echo "  - $REGISTRY/syftbox-dataowner:latest (data owner with Jupyter/Python tools)"
}

# Run main function
main "$@"