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
until mc alias set myminio "$ENDPOINT" "$ROOT_USER" "$ROOT_PASS" >/dev/null 2>&1; do
  sleep 1
done

# Create bucket if it doesn't exist
if ! mc ls myminio/"$BUCKET" >/dev/null 2>&1; then
  mc mb myminio/"$BUCKET" --region "$REGION"
  echo "✅ Bucket '$BUCKET' created"
else
  echo "ℹ️  Bucket '$BUCKET' already exists"
fi

# Create service account if it doesn't exist
if ! mc admin user svcacct info myminio "$ACCESS_KEY" >/dev/null 2>&1; then
  mc admin user svcacct add myminio "$ROOT_USER" \
    --access-key "$ACCESS_KEY" \
    --secret-key "$SECRET_KEY"

  echo "✅ Service account '$ACCESS_KEY' created and given readwrite policy"
else
  echo "ℹ️  Service account '$ACCESS_KEY' already exists"
fi
