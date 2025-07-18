# Bastion VMs for accessing Jupyter notebooks via IAP
# These are tiny VMs that provide secure access to pod Jupyter instances

# Bastion VM for High Pod (always created)
resource "google_compute_instance" "bastion_high" {
  name         = "${var.cluster_name}-bastion-high"
  machine_type = "e2-micro"  # Tiny VM for bastion
  zone         = var.zone

  boot_disk {
    initialize_params {
      image = "debian-cloud/debian-11"
      size  = 10
      type  = "pd-standard"
    }
  }

  network_interface {
    network    = google_compute_network.vpc.name
    subnetwork = google_compute_subnetwork.subnet.name
    # No external IP - access via IAP only
  }

  tags = ["syftbox", "bastion", "high-bastion"]

  metadata = {
    enable-oslogin = "TRUE"
  }

  metadata_startup_script = <<-EOF
    #!/bin/bash
    apt-get update
    apt-get install -y kubectl
    
    # Configure kubectl to connect to cluster
    echo 'gcloud container clusters get-credentials ${var.cluster_name} --zone ${var.zone} --project ${var.project_id}' > /home/debian/configure-kubectl.sh
    chmod +x /home/debian/configure-kubectl.sh
    
    # Create port-forward script for High pod
    cat > /home/debian/forward-high-jupyter.sh << 'SCRIPT'
#!/bin/bash
echo "Setting up port forward to High pod Jupyter..."
kubectl port-forward --address 0.0.0.0 service/syftbox-high 8889:8889
SCRIPT
    chmod +x /home/debian/forward-high-jupyter.sh
    
    # Create helper script
    cat > /home/debian/access-high-jupyter.sh << 'SCRIPT'
#!/bin/bash
echo "To access High pod Jupyter notebook:"
echo "1. Run: gcloud compute ssh ${var.cluster_name}-bastion-high --zone=${var.zone} --tunnel-through-iap --project=${var.project_id} -- -L 8889:localhost:8889 -N"
echo "2. Open browser to: http://localhost:8889"
SCRIPT
    chmod +x /home/debian/access-high-jupyter.sh
    chown debian:debian /home/debian/*.sh
  EOF

  service_account {
    email  = google_service_account.bastion.email
    scopes = ["cloud-platform"]
  }

  depends_on = [
    google_container_cluster.primary,
    google_project_service.apis
  ]
}

# Bastion VM for Low Pod (always created)
resource "google_compute_instance" "bastion_low" {
  name         = "${var.cluster_name}-bastion-low"
  machine_type = "e2-micro"  # Tiny VM for bastion
  zone         = var.zone

  boot_disk {
    initialize_params {
      image = "debian-cloud/debian-11"
      size  = 10
      type  = "pd-standard"
    }
  }

  network_interface {
    network    = google_compute_network.vpc.name
    subnetwork = google_compute_subnetwork.subnet.name
    # No external IP - access via IAP only
  }

  tags = ["syftbox", "bastion", "low-bastion"]

  metadata = {
    enable-oslogin = "TRUE"
  }

  metadata_startup_script = <<-EOF
    #!/bin/bash
    apt-get update
    apt-get install -y kubectl
    
    # Configure kubectl to connect to cluster
    echo 'gcloud container clusters get-credentials ${var.cluster_name} --zone ${var.zone} --project ${var.project_id}' > /home/debian/configure-kubectl.sh
    chmod +x /home/debian/configure-kubectl.sh
    
    # Create port-forward script for Low pod
    cat > /home/debian/forward-low-jupyter.sh << 'SCRIPT'
#!/bin/bash
echo "Setting up port forward to Low pod Jupyter..."
kubectl port-forward --address 0.0.0.0 service/syftbox-low 8888:80
SCRIPT
    chmod +x /home/debian/forward-low-jupyter.sh
    
    # Create helper script
    cat > /home/debian/access-low-jupyter.sh << 'SCRIPT'
#!/bin/bash
echo "To access Low pod Jupyter notebook:"
echo "1. Run: gcloud compute ssh ${var.cluster_name}-bastion-low --zone=${var.zone} --tunnel-through-iap --project=${var.project_id} -- -L 8888:localhost:8888 -N"
echo "2. Open browser to: http://localhost:8888/jupyter/"
SCRIPT
    chmod +x /home/debian/access-low-jupyter.sh
    chown debian:debian /home/debian/*.sh
  EOF

  service_account {
    email  = google_service_account.bastion.email
    scopes = ["cloud-platform"]
  }

  depends_on = [
    google_container_cluster.primary,
    google_project_service.apis
  ]
}

# Bastion VM for Data Scientist VM (conditional - only if DS VM enabled and no public IP)
resource "google_compute_instance" "bastion_ds_vm" {
  count = var.enable_ds_vm && !var.ds_vm_public_ip ? 1 : 0
  
  name         = "${var.cluster_name}-bastion-ds-vm"
  machine_type = "e2-micro"  # Tiny VM for bastion
  zone         = var.zone

  boot_disk {
    initialize_params {
      image = "debian-cloud/debian-11"
      size  = 10
      type  = "pd-standard"
    }
  }

  network_interface {
    network    = google_compute_network.vpc.name
    subnetwork = google_compute_subnetwork.subnet.name
    # No external IP - access via IAP only
  }

  tags = ["syftbox", "bastion", "ds-vm-bastion"]

  metadata = {
    enable-oslogin = "TRUE"
  }

  metadata_startup_script = <<-EOF
    #!/bin/bash
    apt-get update
    apt-get install -y kubectl
    
    # Configure kubectl to connect to cluster
    echo 'gcloud container clusters get-credentials ${var.cluster_name} --zone ${var.zone} --project ${var.project_id}' > /home/debian/configure-kubectl.sh
    chmod +x /home/debian/configure-kubectl.sh
    
    # Create port-forward script for DS VM pod
    cat > /home/debian/forward-ds-vm-jupyter.sh << 'SCRIPT'
#!/bin/bash
echo "Setting up port forward to Data Scientist VM Jupyter..."
kubectl port-forward --address 0.0.0.0 service/syftbox-ds-vm 8888:8888
SCRIPT
    chmod +x /home/debian/forward-ds-vm-jupyter.sh
    
    # Create helper script
    cat > /home/debian/access-ds-vm-jupyter.sh << 'SCRIPT'
#!/bin/bash
echo "To access Data Scientist VM Jupyter notebook:"
echo "1. Run: gcloud compute ssh ${var.cluster_name}-bastion-ds-vm --zone=${var.zone} --tunnel-through-iap --project=${var.project_id} -- -L 8888:localhost:8888 -N"
echo "2. Open browser to: http://localhost:8888"
SCRIPT
    chmod +x /home/debian/access-ds-vm-jupyter.sh
    chown debian:debian /home/debian/*.sh
  EOF

  service_account {
    email  = google_service_account.bastion.email
    scopes = ["cloud-platform"]
  }

  depends_on = [
    google_container_cluster.primary,
    google_project_service.apis
  ]
}

# Output the bastion access commands
output "bastion_high_iap_ssh_command" {
  description = "Command to SSH to High pod bastion via IAP"
  value       = "gcloud compute ssh ${google_compute_instance.bastion_high.name} --zone=${var.zone} --tunnel-through-iap --project=${var.project_id}"
}

output "bastion_low_iap_ssh_command" {
  description = "Command to SSH to Low pod bastion via IAP"
  value       = "gcloud compute ssh ${google_compute_instance.bastion_low.name} --zone=${var.zone} --tunnel-through-iap --project=${var.project_id}"
}

output "bastion_ds_vm_iap_ssh_command" {
  description = "Command to SSH to DS VM bastion via IAP (if enabled)"
  value       = var.enable_ds_vm && !var.ds_vm_public_ip ? "gcloud compute ssh ${google_compute_instance.bastion_ds_vm[0].name} --zone=${var.zone} --tunnel-through-iap --project=${var.project_id}" : "DS VM bastion not created (DS VM disabled or has public IP)"
}

output "high_pod_jupyter_tunnel_command" {
  description = "Command to tunnel to High pod Jupyter"
  value       = "gcloud compute ssh ${google_compute_instance.bastion_high.name} --zone=${var.zone} --tunnel-through-iap --project=${var.project_id} -- -L 8889:localhost:8889 -N"
}

output "low_pod_jupyter_tunnel_command" {
  description = "Command to tunnel to Low pod Jupyter"
  value       = "gcloud compute ssh ${google_compute_instance.bastion_low.name} --zone=${var.zone} --tunnel-through-iap --project=${var.project_id} -- -L 8888:localhost:8888 -N"
}

output "ds_vm_jupyter_tunnel_command" {
  description = "Command to tunnel to DS VM Jupyter (if bastion enabled)"
  value       = var.enable_ds_vm && !var.ds_vm_public_ip ? "gcloud compute ssh ${google_compute_instance.bastion_ds_vm[0].name} --zone=${var.zone} --tunnel-through-iap --project=${var.project_id} -- -L 8888:localhost:8888 -N" : "DS VM bastion not available (DS VM disabled or has public IP)"
}