#!/bin/bash
set -e

# Database Initialization Script

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TERRAFORM_DIR="$SCRIPT_DIR/../terraform"

print_info() {
    echo -e "${BLUE}→ $1${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

# Get database connection info from Terraform
get_db_info() {
    print_info "Getting database connection information..."
    
    cd "$TERRAFORM_DIR"
    
    # Get outputs
    DB_HOST=$(terraform output -raw database_host 2>/dev/null || echo "")
    DB_PASSWORD=$(terraform output -raw database_password 2>/dev/null || echo "")
    DB_CONNECTION_NAME=$(terraform output -raw database_connection_name 2>/dev/null || echo "")
    
    cd - > /dev/null
    
    if [ -z "$DB_HOST" ] || [ -z "$DB_PASSWORD" ]; then
        print_error "Could not get database information from Terraform"
        echo "Please ensure infrastructure is deployed first"
        exit 1
    fi
    
    print_success "Retrieved database connection information"
}

# Initialize database schema
init_database_schema() {
    print_info "Initializing database schema..."
    
    # Use Cloud SQL Proxy or direct connection
    export PGPASSWORD="$DB_PASSWORD"
    
    # Create schema
    psql -h "$DB_HOST" -U syftbox -d syftbox << 'EOF'
-- Users table
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    name VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Datasites table
CREATE TABLE IF NOT EXISTS datasites (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    config JSONB DEFAULT '{}',
    status VARCHAR(50) DEFAULT 'active',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, name)
);

-- Apps table
CREATE TABLE IF NOT EXISTS apps (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) UNIQUE NOT NULL,
    version VARCHAR(50),
    config JSONB DEFAULT '{}',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Data requests table
CREATE TABLE IF NOT EXISTS data_requests (
    id SERIAL PRIMARY KEY,
    requester_id INTEGER REFERENCES users(id),
    owner_id INTEGER REFERENCES users(id),
    datasite_id INTEGER REFERENCES datasites(id),
    request_data JSONB NOT NULL,
    status VARCHAR(50) DEFAULT 'pending',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_datasites_user_id ON datasites(user_id);
CREATE INDEX IF NOT EXISTS idx_datasites_status ON datasites(status);
CREATE INDEX IF NOT EXISTS idx_data_requests_requester ON data_requests(requester_id);
CREATE INDEX IF NOT EXISTS idx_data_requests_owner ON data_requests(owner_id);
CREATE INDEX IF NOT EXISTS idx_data_requests_status ON data_requests(status);

-- Create updated_at trigger function
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Add triggers for updated_at
CREATE TRIGGER update_users_updated_at BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_datasites_updated_at BEFORE UPDATE ON datasites
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_apps_updated_at BEFORE UPDATE ON apps
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_data_requests_updated_at BEFORE UPDATE ON data_requests
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Insert sample data (optional)
INSERT INTO users (email, name) VALUES 
    ('admin@syftbox.local', 'Admin User'),
    ('demo@syftbox.local', 'Demo User')
ON CONFLICT (email) DO NOTHING;

-- Grant permissions
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO syftbox;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO syftbox;
EOF
    
    print_success "Database schema initialized"
}

# Create Kubernetes secret
create_k8s_secret() {
    print_info "Creating Kubernetes secret for database credentials..."
    
    # Create secret
    kubectl create secret generic syftbox-database \
        --from-literal=host="$DB_HOST" \
        --from-literal=port="5432" \
        --from-literal=database="syftbox" \
        --from-literal=username="syftbox" \
        --from-literal=password="$DB_PASSWORD" \
        --from-literal=connection-name="$DB_CONNECTION_NAME" \
        --namespace=syftbox \
        --dry-run=client -o yaml | kubectl apply -f -
    
    print_success "Kubernetes secret created"
}

# Verify database connection
verify_connection() {
    print_info "Verifying database connection..."
    
    export PGPASSWORD="$DB_PASSWORD"
    
    # Test connection
    if psql -h "$DB_HOST" -U syftbox -d syftbox -c "SELECT version();" > /dev/null 2>&1; then
        print_success "Database connection successful"
        
        # Show table count
        TABLE_COUNT=$(psql -h "$DB_HOST" -U syftbox -d syftbox -t -c "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public';")
        print_success "Database has $TABLE_COUNT tables"
    else
        print_error "Failed to connect to database"
        exit 1
    fi
}

# Main function
main() {
    print_info "Starting database initialization..."
    
    # Get database info
    get_db_info
    
    # Initialize schema
    init_database_schema
    
    # Create Kubernetes secret
    create_k8s_secret
    
    # Verify connection
    verify_connection
    
    print_success "Database initialization complete!"
    echo ""
    echo "Database details:"
    echo "  Host: $DB_HOST"
    echo "  Database: syftbox"
    echo "  Username: syftbox"
    echo ""
    echo "To connect from the data owner pod:"
    echo "  kubectl exec -it deploy/syftbox-data-owner -n syftbox -- db-connect"
}

# Run main function
main "$@"