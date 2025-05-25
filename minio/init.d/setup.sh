#!/bin/sh

# Root credentials
ROOT_USER="minioadmin"
ROOT_PASS="minioadmin"

# Config values
BUCKET="syftbox-local"
REGION="us-east-1"
ENDPOINT="http://localhost:9000"
ACCESS_KEY="ptSLdKiwOi2LYQFZYEZ6"
SECRET_KEY="GMDvYrAhWDkB2DyFMn8gU8I8Bg0fT3JGT6iEB7P8"

# Wait for MinIO to be ready using mc's own alias check
echo "Waiting for MinIO at $ENDPOINT to be ready..."
until mc alias set local/ "$ENDPOINT" "$ROOT_USER" "$ROOT_PASS" >/dev/null 2>&1; do
  sleep 1
done

# Create bucket if it doesn't exist
mc mb local/"$BUCKET" --region "$REGION" --with-versioning --ignore-existing

# Create service account if it doesn't exist
if ! mc admin user accesskey info local/ "$ACCESS_KEY" >/dev/null 2>&1; then
  mc admin accesskey create local/ \
    --access-key "$ACCESS_KEY" \
    --secret-key "$SECRET_KEY" \
    --name local-dev

  echo "✅ Access Key '$ACCESS_KEY' created"
else
  echo "ℹ️ Access Key '$ACCESS_KEY' already exists"
fi
