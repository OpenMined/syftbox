#!/bin/bash
set -e

# Setup Prerequisites Script

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_header() {
    echo -e "\n${BLUE}==== $1 ====${NC}\n"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${BLUE}→ $1${NC}"
}

# Detect OS
detect_os() {
    if [[ "$OSTYPE" == "linux-gnu"* ]]; then
        if [ -f /etc/debian_version ]; then
            OS="debian"
        elif [ -f /etc/redhat-release ]; then
            OS="redhat"
        else
            OS="linux"
        fi
    elif [[ "$OSTYPE" == "darwin"* ]]; then
        OS="macos"
    else
        print_error "Unsupported OS: $OSTYPE"
        exit 1
    fi
    print_success "Detected OS: $OS"
}

# Install Docker
install_docker() {
    print_header "Installing Docker"
    
    if command -v docker &> /dev/null; then
        print_success "Docker already installed"
        return
    fi
    
    case $OS in
        debian)
            print_info "Installing Docker on Debian/Ubuntu..."
            curl -fsSL https://get.docker.com | sh
            sudo usermod -aG docker $USER
            ;;
        macos)
            print_info "Please install Docker Desktop from: https://www.docker.com/products/docker-desktop"
            ;;
        *)
            print_error "Please install Docker manually"
            ;;
    esac
}

# Install Google Cloud SDK
install_gcloud() {
    print_header "Installing Google Cloud SDK"
    
    if command -v gcloud &> /dev/null; then
        print_success "gcloud already installed"
        return
    fi
    
    case $OS in
        debian)
            print_info "Installing gcloud on Debian/Ubuntu..."
            echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" | sudo tee -a /etc/apt/sources.list.d/google-cloud-sdk.list
            curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | sudo apt-key --keyring /usr/share/keyrings/cloud.google.gpg add -
            sudo apt-get update && sudo apt-get install -y google-cloud-sdk
            ;;
        macos)
            print_info "Installing gcloud on macOS..."
            if command -v brew &> /dev/null; then
                brew install --cask google-cloud-sdk
            else
                print_info "Please install from: https://cloud.google.com/sdk/docs/install"
            fi
            ;;
        *)
            print_info "Please install gcloud from: https://cloud.google.com/sdk/docs/install"
            ;;
    esac
}

# Install kubectl
install_kubectl() {
    print_header "Installing kubectl"
    
    if command -v kubectl &> /dev/null; then
        print_success "kubectl already installed"
        return
    fi
    
    print_info "Installing kubectl..."
    curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/$(uname -s | tr '[:upper:]' '[:lower:]')/$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')/kubectl"
    chmod +x kubectl
    sudo mv kubectl /usr/local/bin/
}

# Install Terraform
install_terraform() {
    print_header "Installing Terraform"
    
    if command -v terraform &> /dev/null; then
        print_success "Terraform already installed"
        return
    fi
    
    print_info "Installing Terraform..."
    TERRAFORM_VERSION="1.5.7"
    
    case $OS in
        debian|linux)
            wget -q https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_amd64.zip
            unzip terraform_${TERRAFORM_VERSION}_linux_amd64.zip
            sudo mv terraform /usr/local/bin/
            rm terraform_${TERRAFORM_VERSION}_linux_amd64.zip
            ;;
        macos)
            if command -v brew &> /dev/null; then
                brew tap hashicorp/tap
                brew install hashicorp/tap/terraform
            else
                wget -q https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_darwin_amd64.zip
                unzip terraform_${TERRAFORM_VERSION}_darwin_amd64.zip
                sudo mv terraform /usr/local/bin/
                rm terraform_${TERRAFORM_VERSION}_darwin_amd64.zip
            fi
            ;;
    esac
}

# Install Helm
install_helm() {
    print_header "Installing Helm"
    
    if command -v helm &> /dev/null; then
        print_success "Helm already installed"
        return
    fi
    
    print_info "Installing Helm..."
    curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
}

# Configure gcloud
configure_gcloud() {
    print_header "Configuring Google Cloud SDK"
    
    if gcloud auth list --filter=status:ACTIVE --format="value(account)" | grep -q .; then
        print_success "Already authenticated with gcloud"
    else
        print_info "Please authenticate with Google Cloud..."
        gcloud auth login
    fi
    
    print_info "Setting up application default credentials..."
    gcloud auth application-default login
}

# Main function
main() {
    print_header "Setting up SyftBox Deployment Prerequisites"
    
    # Detect OS
    detect_os
    
    # Install required tools
    install_docker
    install_gcloud
    install_kubectl
    install_terraform
    install_helm
    
    # Configure gcloud
    configure_gcloud
    
    print_header "Prerequisites Setup Complete!"
    
    # Verify installations
    print_info "Verifying installations..."
    echo ""
    echo "Tool versions:"
    docker --version 2>/dev/null || echo "Docker: Not installed"
    gcloud --version 2>/dev/null | head -1 || echo "gcloud: Not installed"
    kubectl version --client 2>/dev/null || echo "kubectl: Not installed"
    terraform --version 2>/dev/null | head -1 || echo "Terraform: Not installed"
    helm version --short 2>/dev/null || echo "Helm: Not installed"
    
    echo ""
    print_success "All prerequisites installed!"
    print_info "Next steps:"
    echo "  1. Set your GCP project: export PROJECT_ID=your-project-id"
    echo "  2. Run: ./deploy.sh deploy"
}

# Run main function
main "$@"