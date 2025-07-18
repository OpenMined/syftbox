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

# Docker Hub configuration
DOCKER_ORGANIZATION="${DOCKER_ORGANIZATION:-openmined}"
REGISTRY="docker.io/${DOCKER_ORGANIZATION}"

# Parse command line arguments
FORCE_BUILD=false
if [ "$1" = "--force" ]; then
    FORCE_BUILD=true
    shift
fi

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
    # (Currently no temporary files to clean up)
    :
}

# Set trap for cleanup
trap cleanup EXIT

# Prepare build context
prepare_build_context() {
    print_info "Preparing build context..."
    
    # Only validate SyftBox repository structure if cache server is enabled
    if [ "${DEPLOY_CACHE_SERVER}" == "true" ]; then
        # Validate that we have the SyftBox repository structure for cache server/data owner builds
        if [ ! -f "$BUILD_CONTEXT/go.mod" ] || [ ! -f "$BUILD_CONTEXT/go.sum" ]; then
            print_error "SyftBox repository structure not found at: $BUILD_CONTEXT"
            print_error "Expected to find go.mod and go.sum in the parent directory of the deployment."
            exit 1
        fi
    fi
    
    print_success "Build context ready"
}

# Build deployment images
build_deployment_images() {
    print_info "Building deployment images..."
    
    # Build High pod image
    print_info "Building syftbox-high pod..."
    docker build -t "$REGISTRY/syftbox-high:latest" \
        -f "$DOCKER_DIR/Dockerfile.high" "$DOCKER_DIR"
    
    # Build Low pod image (with SyftBox client)
    if [ -f "$BUILD_CONTEXT/go.mod" ] && [ -f "$BUILD_CONTEXT/go.sum" ]; then
        print_info "Building syftbox-low pod (with SyftBox client)..."
        docker build -t "$REGISTRY/syftbox-low:latest" \
            -f "$DOCKER_DIR/Dockerfile.low-syftbox" "$BUILD_CONTEXT"
    else
        print_error "SyftBox source not found at $BUILD_CONTEXT - cannot build Low pod with SyftBox"
        print_error "Low pod with SyftBox requires SyftBox source code to build the client"
        exit 1
    fi
    
    # Build DS VM image (requires SyftBox source)
    if [ "${DEPLOY_DS_VM}" == "true" ]; then
        if [ -f "$BUILD_CONTEXT/go.mod" ] && [ -f "$BUILD_CONTEXT/go.sum" ]; then
            print_info "Building syftbox-ds-vm pod..."
            docker build -t "$REGISTRY/syftbox-ds-vm:latest" \
                -f "$DOCKER_DIR/Dockerfile.ds-vm" "$BUILD_CONTEXT"
        else
            print_error "SyftBox source not found at $BUILD_CONTEXT - cannot build DS VM"
            print_error "DS VM requires SyftBox source code to build the client"
            exit 1
        fi
    fi
    
    # Only build cache server and data owner images if cache server is enabled
    if [ "${DEPLOY_CACHE_SERVER}" == "true" ]; then
        print_info "Building cache server images..."
        
        # Build cache server image (from syftbox source)
        print_info "Building syftbox-cache-server..."
        docker build -t "$REGISTRY/syftbox-cache-server:latest" \
            -f "$DOCKER_DIR/Dockerfile.server" "$BUILD_CONTEXT"
        
        # Build data owner client image (from syftbox source)
        print_info "Building syftbox-dataowner..."
        docker build -t "$REGISTRY/syftbox-dataowner:latest" \
            -f "$DOCKER_DIR/Dockerfile.dataowner" "$BUILD_CONTEXT"
    else
        print_info "Cache server disabled - skipping cache server image builds"
    fi
}

# Check if images already exist on Docker Hub
check_images_exist() {
    print_info "Checking if images already exist on Docker Hub..."
    
    local images=(
        "$REGISTRY/syftbox-high:latest"
        "$REGISTRY/syftbox-low:latest"
    )
    
    # Add DS VM image if enabled
    if [ "${DEPLOY_DS_VM}" == "true" ]; then
        images+=(
            "$REGISTRY/syftbox-ds-vm:latest"
        )
    fi
    
    # Add cache server images if enabled
    if [ "${DEPLOY_CACHE_SERVER}" == "true" ]; then
        images+=(
            "$REGISTRY/syftbox-cache-server:latest"
            "$REGISTRY/syftbox-dataowner:latest"
        )
    fi
    
    local all_exist=true
    for image in "${images[@]}"; do
        if docker manifest inspect "$image" &>/dev/null; then
            print_success "Image $image exists on Docker Hub"
        else
            print_info "Image $image does not exist on Docker Hub"
            all_exist=false
        fi
    done
    
    if [ "$all_exist" = true ] && [ "$FORCE_BUILD" != true ]; then
        print_success "All images already exist on Docker Hub. Use --force to rebuild."
        echo ""
        echo "Images available at:"
        for image in "${images[@]}"; do
            echo "  - $image"
        done
        exit 0
    fi
}

# Login to Docker Hub
login_to_registry() {
    print_info "Checking Docker Hub authentication..."
    
    # Check if already logged in by testing a simple command
    if docker info >/dev/null 2>&1; then
        print_info "Using existing Docker authentication"
        print_info "Pushing to organization: $DOCKER_ORGANIZATION"
        print_success "Docker authentication ready"
        return 0
    fi
    
    # If not logged in, try to login with credentials
    if [ -n "$DOCKER_USERNAME" ] && [ -n "$DOCKER_PASSWORD" ]; then
        print_info "Logging in as user: $DOCKER_USERNAME"
        print_info "Pushing to organization: $DOCKER_ORGANIZATION"
        
        # Login to Docker Hub
        echo "$DOCKER_PASSWORD" | docker login docker.io --username "$DOCKER_USERNAME" --password-stdin
        
        print_success "Successfully logged in to Docker Hub"
    else
        print_info "No credentials provided - assuming already logged in"
        print_info "Pushing to organization: $DOCKER_ORGANIZATION"
        print_warning "If push fails, run: docker login docker.io"
    fi
}

# Push images
push_images() {
    print_info "Pushing images to registry..."
    
    local images=(
        "$REGISTRY/syftbox-high:latest"
        "$REGISTRY/syftbox-low:latest"
    )
    
    # Add DS VM image if enabled
    if [ "${DEPLOY_DS_VM}" == "true" ]; then
        images+=(
            "$REGISTRY/syftbox-ds-vm:latest"
        )
    fi
    
    # Add cache server images if enabled
    if [ "${DEPLOY_CACHE_SERVER}" == "true" ]; then
        images+=(
            "$REGISTRY/syftbox-cache-server:latest"
            "$REGISTRY/syftbox-dataowner:latest"
        )
    fi
    
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
        "$REGISTRY/syftbox-high:latest"
        "$REGISTRY/syftbox-low:latest"
    )
    
    # Add DS VM image if enabled
    if [ "${DEPLOY_DS_VM}" == "true" ]; then
        images+=(
            "$REGISTRY/syftbox-ds-vm:latest"
        )
    fi
    
    # Add cache server images if enabled
    if [ "${DEPLOY_CACHE_SERVER}" == "true" ]; then
        images+=(
            "$REGISTRY/syftbox-cache-server:latest"
            "$REGISTRY/syftbox-dataowner:latest"
        )
    fi
    
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
    
    # Check if images already exist (skip if --force is used)
    check_images_exist
    
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
    echo "  - $REGISTRY/syftbox-high:latest (high pod - private operations)"
    echo "  - $REGISTRY/syftbox-low:latest (low pod - web services + SyftBox)"
    if [ "${DEPLOY_CACHE_SERVER}" == "true" ]; then
        echo "  - $REGISTRY/syftbox-cache-server:latest (cache server - no DB dependencies)"
        echo "  - $REGISTRY/syftbox-dataowner:latest (data owner with Jupyter/Python tools)"
    fi
}

# Run main function
main "$@"