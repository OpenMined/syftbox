#!/bin/bash
set -e

# Standalone Build Script for SyftBox Docker Images
# This script builds and pushes all SyftBox images to Docker Hub

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DOCKER_DIR="$SCRIPT_DIR/docker"
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

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

# Check if images already exist on Docker Hub
check_images_exist() {
    print_info "Checking if images already exist on Docker Hub..."
    
    local images=(
        "$REGISTRY/syftbox-high:latest"
        "$REGISTRY/syftbox-low:latest"
        "$REGISTRY/syftbox-ds-vm:latest"
        "$REGISTRY/syftbox-cache-server:latest"
        "$REGISTRY/syftbox-dataowner:latest"
    )
    
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

# Build all images
build_all_images() {
    print_info "Building all SyftBox Docker images..."
    
    # Build High pod image
    print_info "Building syftbox-high (private operations)..."
    docker build -t "$REGISTRY/syftbox-high:latest" \
        -f "$DOCKER_DIR/Dockerfile.high" "$DOCKER_DIR"
    
    # Build Low pod image (with SyftBox client)
    if [ -f "$BUILD_CONTEXT/go.mod" ] && [ -f "$BUILD_CONTEXT/go.sum" ]; then
        print_info "Building syftbox-low (web services + SyftBox)..."
        docker build -t "$REGISTRY/syftbox-low:latest" \
            -f "$DOCKER_DIR/Dockerfile.low-syftbox" "$BUILD_CONTEXT"
    else
        print_warning "SyftBox source not found at $BUILD_CONTEXT - building Low pod without SyftBox"
        print_info "Building syftbox-low (web services only)..."
        docker build -t "$REGISTRY/syftbox-low:latest" \
            -f "$DOCKER_DIR/Dockerfile.low" "$DOCKER_DIR"
    fi
    
    # Build DS VM image (requires SyftBox source)
    if [ -f "$BUILD_CONTEXT/go.mod" ] && [ -f "$BUILD_CONTEXT/go.sum" ]; then
        print_info "Building syftbox-ds-vm (data science + SyftBox)..."
        docker build -t "$REGISTRY/syftbox-ds-vm:latest" \
            -f "$DOCKER_DIR/Dockerfile.ds-vm" "$BUILD_CONTEXT"
    else
        print_warning "SyftBox source not found at $BUILD_CONTEXT - skipping DS VM build"
        print_warning "DS VM requires SyftBox source code to build the client"
    fi
    
    # Build cache server image (requires SyftBox source)
    if [ -f "$BUILD_CONTEXT/go.mod" ] && [ -f "$BUILD_CONTEXT/go.sum" ]; then
        print_info "Building syftbox-cache-server (SyftBox server)..."
        docker build -t "$REGISTRY/syftbox-cache-server:latest" \
            -f "$DOCKER_DIR/Dockerfile.server" "$BUILD_CONTEXT"
        
        # Build data owner image (legacy)
        print_info "Building syftbox-dataowner (legacy data owner)..."
        docker build -t "$REGISTRY/syftbox-dataowner:latest" \
            -f "$DOCKER_DIR/Dockerfile.dataowner" "$BUILD_CONTEXT"
    else
        print_warning "SyftBox source not found at $BUILD_CONTEXT - skipping cache server builds"
        print_warning "To build cache server images, run this script from the SyftBox deployment directory"
    fi
}

# Push all images
push_all_images() {
    print_info "Pushing all images to Docker Hub..."
    
    local images=(
        "$REGISTRY/syftbox-high:latest"
        "$REGISTRY/syftbox-low:latest"
        "$REGISTRY/syftbox-ds-vm:latest"
    )
    
    # Add cache server images if they exist
    if docker image inspect "$REGISTRY/syftbox-cache-server:latest" &>/dev/null; then
        images+=(
            "$REGISTRY/syftbox-cache-server:latest"
            "$REGISTRY/syftbox-dataowner:latest"
        )
    fi
    
    for image in "${images[@]}"; do
        if docker image inspect "$image" &>/dev/null; then
            print_info "Pushing $image..."
            docker push "$image"
            print_success "Pushed $image"
        else
            print_warning "Skipping $image (not built)"
        fi
    done
}

# Main function
main() {
    print_info "Starting SyftBox Docker image build and push process..."
    print_info "Target registry: $REGISTRY"
    echo ""
    
    # Check if images already exist (skip if --force is used)
    check_images_exist
    
    # Build all images
    build_all_images
    
    # Login and push
    login_to_registry
    push_all_images
    
    print_success "All images built and pushed successfully!"
    echo ""
    echo "Images available at Docker Hub:"
    echo "  - $REGISTRY/syftbox-high:latest (high pod - private operations)"
    echo "  - $REGISTRY/syftbox-low:latest (low pod - web services + SyftBox)"
    echo "  - $REGISTRY/syftbox-ds-vm:latest (data science VM + SyftBox)"
    if docker image inspect "$REGISTRY/syftbox-cache-server:latest" &>/dev/null; then
        echo "  - $REGISTRY/syftbox-cache-server:latest (cache server - no DB dependencies)"
        echo "  - $REGISTRY/syftbox-dataowner:latest (data owner with Jupyter/Python tools)"
    fi
    echo ""
    echo "Usage in Helm:"
    echo "  --set global.imageRegistry=docker.io/$DOCKER_ORGANIZATION"
}

# Show usage
if [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
    echo "SyftBox Docker Image Build Script"
    echo ""
    echo "Usage: $0 [--force] [--help]"
    echo ""
    echo "Options:"
    echo "  --force    Force rebuild even if images exist"
    echo "  --help     Show this help message"
    echo ""
    echo "Environment variables:"
    echo "  DOCKER_USERNAME     Docker Hub username (required)"
    echo "  DOCKER_PASSWORD     Docker Hub password or token (required)"
    echo "  DOCKER_ORGANIZATION Docker Hub organization (default: openmined)"
    echo ""
    echo "Example:"
    echo "  export DOCKER_USERNAME=your_username"
    echo "  export DOCKER_PASSWORD=your_token_here"
    echo "  ./build-images.sh"
    exit 0
fi

# Run main function
main "$@"