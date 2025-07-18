terraform {
  required_version = ">= 1.0"
  
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 4.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.1"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

# Enable required APIs
resource "google_project_service" "apis" {
  for_each = toset([
    "compute.googleapis.com",
    "container.googleapis.com",
    "sqladmin.googleapis.com",
    "servicenetworking.googleapis.com",
    "artifactregistry.googleapis.com",
    "iap.googleapis.com"
  ])
  
  project = var.project_id
  service = each.value
  
  disable_dependent_services = true
  disable_on_destroy         = false
}

# Artifact Registry
resource "google_artifact_registry_repository" "syftbox" {
  depends_on = [google_project_service.apis]
  
  location      = var.region
  repository_id = "syftbox"
  description   = "SyftBox Docker images"
  format        = "DOCKER"
  
  # Allow anonymous pulling for easier access
  labels = {
    purpose = "syftbox-deployment"
  }
}