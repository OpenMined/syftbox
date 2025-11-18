# SyftBox GKE Deployment - New Architecture

This document describes the updated SyftBox deployment architecture for Google Kubernetes Engine (GKE).

## Overview

The new architecture simplifies the deployment to focus on two main pods with strict network isolation:

1. **High Pod (Private)** - Outbound connections only, database access
2. **Low Pod (Public)** - Inbound/outbound connections, no database access

## Architecture Diagram

```
┌─────────────────────────────────────────────────────┐
│                  GKE Cluster                        │
├─────────────────────────────────────────────────────┤
│                                                     │
│  ┌─────────────────┐     ┌────────────────────┐   │
│  │    High Pod     │     │     Low Pod        │   │
│  │   (Private)     │────▶│    (Public)        │   │
│  │                 │     │                    │   │
│  │ - Jupyter Lab   │     │ - FastAPI         │   │
│  │ - Data Tools    │     │ - Nginx           │   │
│  │ - DB Access     │     │ - Jupyter Lab     │   │
│  └────────┬────────┘     └────────▲───────────┘   │
│           │                       │                │
│           │              ┌────────┴───────────┐   │
│           │              │   LoadBalancer     │   │
│           │              │  (Internet Access) │   │
│           │              └────────────────────┘   │
│           │                                        │
│  ┌────────▼────────┐                              │
│  │ Private Database│                              │
│  │  (Cloud SQL)    │                              │
│  └─────────────────┘                              │
└─────────────────────────────────────────────────────┘
```

## Pod Specifications

### High Pod (Private)
- **Purpose**: Secure data processing and analysis
- **Network**: Outbound only, no inbound connections
- **Access**: Can reach Low pod and private database
- **Tools**: Jupyter Lab, pandas, numpy, scipy, scikit-learn, psycopg2
- **Security**: Runs as non-root user (1000)

### Low Pod (Public)  
- **Purpose**: Public API and interface
- **Network**: Accepts inbound connections (with restrictions)
- **Access**: No database access
- **Tools**: FastAPI, Nginx, Jupyter Lab
- **Security**: Runs as non-root user (1000)

## Network Policies

Network policies enforce strict communication rules:

1. **High Pod**:
   - ❌ No inbound traffic (except health checks)
   - ✅ Outbound to Low pod
   - ✅ Outbound to private database
   - ✅ Outbound to internet (for package downloads)

2. **Low Pod**:
   - ✅ Inbound from High pod
   - ✅ Inbound from allowed IPs (configurable)
   - ✅ Outbound to internet
   - ❌ No database access

## Database Configuration

- Single private PostgreSQL database (Cloud SQL)
- Only accessible by High pod
- No public IP address
- Automated backups enabled

## Deployment Options

### Basic Deployment (Default)
```bash
./deploy.sh deploy
```
This deploys:
- High pod
- Low pod  
- Private database
- Network policies

### With Cache Server (Optional)
```bash
./deploy.sh deploy --with-cache
```
This additionally deploys:
- Legacy cache server
- Mock database
- Data owner pods

## Configuration

### Environment Variables
- `PROJECT_ID` - GCP project ID (required)
- `REGION` - GCP region (default: us-central1)
- `ZONE` - GCP zone (default: us-central1-a)
- `DEPLOY_CACHE_SERVER` - Deploy cache server (default: false)

### Helm Values

Key configuration in `helm/syftbox/values.yaml`:

```yaml
# High Pod Configuration
highPod:
  enabled: true
  service:
    type: ClusterIP  # Internal only
  persistence:
    dataSize: 10Gi
    notebooksSize: 10Gi

# Low Pod Configuration  
lowPod:
  enabled: true
  service:
    type: LoadBalancer  # Public access
  persistence:
    dataSize: 5Gi
    notebooksSize: 5Gi

# Network Policies
networkPolicies:
  enabled: true
  allowedIPs: []  # Add allowed IPs/CIDRs

# Database
database:
  enabled: true
  external: true  # Uses Cloud SQL
```

## Access Methods

### High Pod (Jupyter Lab)
```bash
# Via port-forward
kubectl port-forward -n syftbox svc/syftbox-high 8888:8888

# Via bastion host
gcloud compute ssh syftbox-bastion --zone=us-central1-a --tunnel-through-iap -- -L 8888:localhost:8888 -N
```

### Low Pod (API)
```bash
# Via LoadBalancer (get external IP)
kubectl get svc syftbox-low -n syftbox

# Via port-forward
kubectl port-forward -n syftbox svc/syftbox-low 8080:80
```

## Security Features

1. **Network Isolation**: Strict network policies control pod communication
2. **No Root Access**: All containers run as non-root user
3. **Private Database**: Database has no public IP
4. **IAP Access**: Bastion host only accessible via Identity-Aware Proxy
5. **Secrets Management**: Database credentials stored in Kubernetes secrets

## Monitoring and Troubleshooting

### Check Pod Status
```bash
kubectl get pods -n syftbox
kubectl describe pod <pod-name> -n syftbox
```

### View Logs
```bash
kubectl logs -n syftbox deployment/syftbox-high
kubectl logs -n syftbox deployment/syftbox-low
```

### Access Pods
```bash
kubectl exec -it deployment/syftbox-high -n syftbox -- bash
kubectl exec -it deployment/syftbox-low -n syftbox -- bash
```

### Test Database Connection (from High pod)
```bash
kubectl exec -it deployment/syftbox-high -n syftbox -- db-connect
```

## Cleanup

To destroy all resources:
```bash
./deploy.sh destroy
```

## Migration from Old Architecture

If migrating from the old architecture:

1. Export any necessary data from existing pods
2. Run `./deploy.sh destroy` to clean up old resources
3. Deploy new architecture with `./deploy.sh deploy`
4. Import data into new High pod

## Future Enhancements

- Add ingress controller for HTTPS access
- Implement authentication for Low pod API
- Add monitoring and alerting
- Support for multiple High/Low pod pairs
- Horizontal pod autoscaling