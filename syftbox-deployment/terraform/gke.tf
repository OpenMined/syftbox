# GKE Cluster
resource "google_container_cluster" "primary" {
  name     = var.cluster_name
  location = var.zone
  
  # Remove default node pool
  remove_default_node_pool = true
  initial_node_count       = 1
  
  # Network configuration
  network    = google_compute_network.vpc.name
  subnetwork = google_compute_subnetwork.subnet.name
  
  # IP allocation policy for pods and services
  ip_allocation_policy {
    cluster_secondary_range_name  = "pods"
    services_secondary_range_name = "services"
  }
  
  # Basic cluster settings - pinned to stable version for reproducibility
  min_master_version = "1.32"
  
  depends_on = [
    google_project_service.apis,
    google_compute_network.vpc
  ]
}

# Node pool
resource "google_container_node_pool" "primary_nodes" {
  name       = "${var.cluster_name}-nodes"
  location   = var.zone
  cluster    = google_container_cluster.primary.name
  node_count = var.node_count
  
  node_config {
    machine_type = var.machine_type
    disk_size_gb = 30
    disk_type    = "pd-standard"
    
    # Tags for firewall rules
    tags = ["syftbox"]
    
    # OAuth scopes
    oauth_scopes = [
      "https://www.googleapis.com/auth/cloud-platform"
    ]
    
    # Labels
    labels = {
      environment = "syftbox"
    }
  }
  
  # Management settings
  management {
    auto_repair  = true
    auto_upgrade = true
  }
}