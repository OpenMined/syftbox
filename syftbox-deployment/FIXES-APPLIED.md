# Fixes Applied to SyftBox Deployment

## Issues Fixed

### 1. ✅ MinIO Not Being Disabled Properly
**Problem**: MinIO resources were being deployed even when `minio.enabled=false`
**Solution**: Wrapped entire MinIO template in `{{- if .Values.minio.enabled }}` condition

### 2. ✅ Unnecessary Docker Builds
**Problem**: Cache server images were being built and pushed even when disabled
**Solution**: Added conditional logic to build scripts:
- Only build cache server images when `DEPLOY_CACHE_SERVER=true`
- Only push cache server images when they're built
- Only verify cache server images when they're built

### 3. ✅ Jupyter Lab Port Conflicts
**Problem**: Both High and Low pods were using port 8888 for Jupyter
**Solution**: 
- **High Pod**: Jupyter Lab on port 8889 (internal only)
- **Low Pod**: Jupyter Lab on port 8888 (accessible via nginx at `/jupyter/`)

### 4. ✅ Helm Timeout Issue
**Problem**: Helm was timing out during deployment
**Solution**: Fixed by removing unnecessary MinIO deployment when disabled

## Access Instructions

### High Pod (Private) - Jupyter Lab
```bash
# Via kubectl port-forward
kubectl port-forward -n syftbox svc/syftbox-high 8889:8889

# Via bastion host
gcloud compute ssh syftbox-cluster-bastion --zone=us-central1-a --tunnel-through-iap -- -L 8889:localhost:8889 -N

# Then open: http://localhost:8889
```

### Low Pod (Public) - API & Jupyter
```bash
# API access
kubectl port-forward -n syftbox svc/syftbox-low 8080:80

# Jupyter access
kubectl port-forward -n syftbox svc/syftbox-low 8888:8888

# Via bastion host (both services)
gcloud compute ssh syftbox-cluster-bastion --zone=us-central1-a --tunnel-through-iap -- -L 8080:localhost:80 -L 8888:localhost:8888 -N

# Then open:
# - API: http://localhost:8080
# - Jupyter: http://localhost:8888
```

### Database Access (High Pod Only)
```bash
# Connect to database from High pod
kubectl exec -it deploy/syftbox-high -n syftbox -- db-connect
```

## Deployment Options

### Basic Deployment (High + Low pods only)
```bash
./deploy.sh deploy
```

### With Cache Server
```bash
./deploy.sh deploy --with-cache
```

### With Mock Database
```bash
./deploy.sh deploy --with-mock-db
```

### With Both
```bash
./deploy.sh deploy --with-all
```

## Build Optimizations

- **Cache server disabled**: Only builds High and Low pod images
- **Cache server enabled**: Builds all images (High, Low, Cache Server, Data Owner)
- **MinIO disabled**: No MinIO resources deployed
- **Mock database disabled**: No mock database created in Terraform

## Port Configuration

| Service | Pod | Port | Access |
|---------|-----|------|---------|
| High Pod Jupyter | High | 8889 | Internal only |
| Low Pod API | Low | 80 | Public (LoadBalancer) |
| Low Pod Jupyter | Low | 8888 | Internal + nginx proxy |
| Database | External | 5432 | High pod only |

## Network Policies

- **High Pod**: No inbound except health checks, outbound to Low pod and database
- **Low Pod**: Inbound from High pod and allowed IPs, outbound to internet
- **Database**: Only accessible from High pod

The deployment should now work without timeouts and provide proper access to both Jupyter instances.