#!/bin/bash
set -e

email="${SYFTBOX_EMAIL:-test@example.com}"
server_url="${SYFTBOX_SERVER_URL:-http://server:8080}"
data_dir="/data/${email}"
config_file="${data_dir}/.syftbox/config.json"

mkdir -p "${data_dir}/.syftbox/logs"
mkdir -p "${data_dir}/datasites"

cat > "${config_file}" << EOF
{
  "data_dir": "${data_dir}",
  "email": "${email}",
  "server_url": "${server_url}",
  "client_url": "http://0.0.0.0:7938"
}
EOF

echo "Starting Rust client for ${email} → ${server_url}"
exec syftbox -c "${config_file}" daemon -a "0.0.0.0:7938"
