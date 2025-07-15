# Bastion Host for secure access to cluster services
resource "google_compute_instance" "bastion" {
  name         = "${var.cluster_name}-bastion"
  machine_type = "e2-micro"
  zone         = var.zone

  boot_disk {
    initialize_params {
      image = "ubuntu-os-cloud/ubuntu-2204-lts"
      size  = 20
    }
  }

  network_interface {
    subnetwork = google_compute_subnetwork.subnet.name
    # No external IP - access via IAP only
  }

  metadata = {
    enable-oslogin = "TRUE"
    # ssh-keys = var.bastion_ssh_keys  # Commented out - using OS Login instead
  }

  metadata_startup_script = <<-EOF
    #!/bin/bash
    apt-get update
    apt-get install -y kubectl
    
    # Install Google Cloud SDK
    curl https://sdk.cloud.google.com | bash
    exec -l $SHELL
    
    # Configure kubectl access to the cluster
    gcloud container clusters get-credentials ${var.cluster_name} --zone ${var.zone} --project ${var.project_id}
    
    # Install additional tools for cluster management
    snap install helm --classic
    
    # Create welcome message
    cat > /etc/motd << 'MOTD'
SyftBox Bastion Host
====================

This bastion host provides secure access to the SyftBox cluster.

Available commands:
- kubectl: Kubernetes cluster management
- helm: Helm chart management  
- gcloud: Google Cloud CLI

To access Jupyter:
kubectl port-forward svc/syftbox-data-owner 8888:8888 -n syftbox

Then access http://localhost:8888 from your SSH tunnel.

MOTD
  EOF

  service_account {
    email  = google_service_account.bastion.email
    scopes = ["cloud-platform"]
  }

  tags = ["bastion", "syftbox"]

  depends_on = [
    google_container_cluster.primary,
    google_service_account.bastion
  ]
}

# Service account for bastion host
resource "google_service_account" "bastion" {
  account_id   = "${var.cluster_name}-bastion"
  display_name = "SyftBox Bastion Host Service Account"
}

# IAM binding for bastion to access GKE
resource "google_project_iam_binding" "bastion_gke_access" {
  project = var.project_id
  role    = "roles/container.admin"
  members = [
    "serviceAccount:${google_service_account.bastion.email}"
  ]
}

# Firewall rule for IAP SSH access to bastion
resource "google_compute_firewall" "bastion_iap_ssh" {
  name    = "${var.cluster_name}-bastion-iap-ssh"
  network = google_compute_network.vpc.name

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  # IAP source ranges for SSH tunneling
  source_ranges = ["35.235.240.0/20"]
  target_tags   = ["bastion"]
}

# Direct SSH access removed - IAP-only access for security