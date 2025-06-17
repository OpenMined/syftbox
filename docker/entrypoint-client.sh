#!/bin/bash
set -e

# Function to setup client configuration
setup_client() {
    local email="$1"
    local server_url="${SYFTBOX_SERVER_URL:-http://syftbox-server:8080}"
    local client_dir="/data/clients/${email}"
    local config_file="${client_dir}/config.json"
    local data_dir="${client_dir}/SyftBox"
    
    # Create directories if they don't exist
    mkdir -p "${client_dir}"
    mkdir -p "${data_dir}"
    
    # Set environment variables for this session
    export SYFTBOX_CONFIG_PATH="${config_file}"
    export SYFTBOX_DATA_DIR="${data_dir}"
    export SYFTBOX_EMAIL="${email}"
    export SYFTBOX_SERVER_URL="${server_url}"
    
    # For local dev, create a simple config that bypasses auth
    if [[ "${server_url}" == *"syftbox-server"* ]] || [[ "${server_url}" == *"localhost"* ]] || [[ "${server_url}" == *"127.0.0.1"* ]]; then
        echo "Setting up local dev config (auth bypass)"
        cat > "${config_file}" << EOF
{
  "data_dir": "${data_dir}",
  "email": "${email}",
  "server_url": "${server_url}",
  "client_url": "http://localhost:7938"
}
EOF
    fi
    
    # Create symlinks for easier access in container
    ln -sf "${config_file}" /root/.syftbox/config.json
    ln -sf "${data_dir}" /root/SyftBox
    
    echo "Client setup for ${email}:"
    echo "  Config: ${config_file}"
    echo "  Data: ${data_dir}"
    echo "  Server: ${server_url}"
}

# If email is provided as first argument, setup the client
if [[ "$1" =~ ^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$ ]]; then
    email="$1"
    shift
    setup_client "${email}"
    
    # If no additional command provided, run daemon
    if [ $# -eq 0 ]; then
        echo "Starting daemon for ${email}..."
        exec ./syftbox daemon
    else
        # Run the provided command
        exec ./syftbox "$@"
    fi
elif [ "$1" = "login" ] && [ -n "$2" ]; then
    # Handle login command specially
    email="$2"
    shift 2
    setup_client "${email}"
    
    # For local dev servers, skip actual login and just setup config
    if [[ "${SYFTBOX_SERVER_URL:-http://syftbox-server:8080}" == *"syftbox-server"* ]] || [[ "${SYFTBOX_SERVER_URL:-http://syftbox-server:8080}" == *"localhost"* ]] || [[ "${SYFTBOX_SERVER_URL:-http://syftbox-server:8080}" == *"127.0.0.1"* ]]; then
        echo "Local dev server detected - skipping OTP login"
        echo "Config created at: ${SYFTBOX_CONFIG_PATH}"
        echo "You can now run: just run-docker-client-daemon ${email}"
        exit 0
    else
        exec ./syftbox login "$@"
    fi
else
    # Pass through to syftbox
    exec ./syftbox "$@"
fi