variable "project_id" {
  description = "The GCP project ID"
  type        = string
}

variable "region" {
  description = "The GCP region"
  type        = string
  default     = "us-central1"
}

variable "zone" {
  description = "The GCP zone"
  type        = string
  default     = "us-central1-a"
}

variable "cluster_name" {
  description = "The name of the GKE cluster"
  type        = string
  default     = "syftbox-cluster"
}

variable "node_count" {
  description = "Number of nodes in the GKE cluster"
  type        = number
  default     = 3
}

variable "machine_type" {
  description = "Machine type for GKE nodes"
  type        = string
  default     = "e2-standard-4"
}

variable "database_tier" {
  description = "The tier for the Cloud SQL instance"
  type        = string
  default     = "db-f1-micro"
}

variable "bastion_ssh_keys" {
  description = "SSH public keys for bastion host access (format: 'username:ssh-rsa AAAAB3Nz...')"
  type        = string
  default     = ""
}

variable "database_deletion_protection" {
  description = "Enable deletion protection for Cloud SQL databases"
  type        = bool
  default     = false
}

variable "enable_mock_database" {
  description = "Enable mock database (for testing/public data)"
  type        = bool
  default     = false
}

variable "enable_ds_vm" {
  description = "Enable Data Scientist VM pod"
  type        = bool
  default     = false
}

variable "ds_vm_public_ip" {
  description = "Give Data Scientist VM pod a public IP (LoadBalancer service). If false, uses bastion VM."
  type        = bool
  default     = false
}

variable "low_pod_email" {
  description = "Email address for Low pod SyftBox client"
  type        = string
  default     = "lowpod@syftbox.local"
}

variable "ds_vm_email" {
  description = "Email address for Data Scientist VM SyftBox client"
  type        = string
  default     = "datascientist@syftbox.local"
}

# variable "bastion_allowed_ips" {
#   description = "IP addresses allowed to SSH to bastion host"
#   type        = list(string)
#   default     = ["0.0.0.0/0"]  # Not needed with IAP-only access
# }