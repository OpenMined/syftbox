output "cluster_endpoint" {
  description = "GKE cluster endpoint"
  value       = google_container_cluster.primary.endpoint
  sensitive   = true
}

output "kubectl_config" {
  description = "Command to configure kubectl"
  value       = "gcloud container clusters get-credentials ${var.cluster_name} --zone ${var.zone} --project ${var.project_id}"
}

output "private_database_host" {
  description = "Private Cloud SQL instance private IP address"
  value       = google_sql_database_instance.private.private_ip_address
}

output "private_database_password" {
  description = "Private database password"
  value       = random_password.private_db_password.result
  sensitive   = true
}

output "private_database_connection_name" {
  description = "Private Cloud SQL connection name"
  value       = google_sql_database_instance.private.connection_name
}

# Mock database outputs (only if enabled)
output "mock_database_host" {
  description = "Mock Cloud SQL instance private IP address"
  value       = var.enable_mock_database ? google_sql_database_instance.mock[0].private_ip_address : ""
}

output "mock_database_public_ip" {
  description = "Mock Cloud SQL instance public IP address"
  value       = var.enable_mock_database ? google_sql_database_instance.mock[0].public_ip_address : ""
}

output "mock_database_password" {
  description = "Mock database password"
  value       = var.enable_mock_database ? random_password.mock_db_password.result : ""
  sensitive   = true
}

output "mock_database_connection_name" {
  description = "Mock Cloud SQL connection name"
  value       = var.enable_mock_database ? google_sql_database_instance.mock[0].connection_name : ""
}

output "artifact_registry_url" {
  description = "Artifact Registry URL for Docker images"
  value       = "${google_artifact_registry_repository.syftbox.location}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.syftbox.repository_id}"
}

output "bastion_instance_name" {
  description = "Name of the bastion host instance"
  value       = google_compute_instance.bastion.name
}

output "bastion_zone" {
  description = "Zone where bastion host is deployed"
  value       = google_compute_instance.bastion.zone
}

output "bastion_iap_ssh_command" {
  description = "Command to SSH to bastion host via internal hostname"
  value       = "gcloud compute ssh ${google_compute_instance.bastion.name} --project ${var.project_id} --zone ${google_compute_instance.bastion.zone} -- -o Hostname=nic0.${google_compute_instance.bastion.name}.${google_compute_instance.bastion.zone}.${substr(google_compute_instance.bastion.zone, -1, 1)}.${var.project_id}.internal.gcpnode.com"
}

output "bastion_iap_tunnel_command" {
  description = "Example command to create tunnel for Jupyter access via internal hostname"
  value       = "gcloud compute ssh ${google_compute_instance.bastion.name} --project ${var.project_id} --zone ${google_compute_instance.bastion.zone} -- -o Hostname=nic0.${google_compute_instance.bastion.name}.${google_compute_instance.bastion.zone}.${substr(google_compute_instance.bastion.zone, -1, 1)}.${var.project_id}.internal.gcpnode.com -L 8888:localhost:8888 -N"
}