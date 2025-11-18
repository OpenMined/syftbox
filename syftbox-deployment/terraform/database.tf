# Random password for database
resource "random_password" "private_db_password" {
  length  = 16
  special = true
}

# PRIVATE PostgreSQL instance - only accessible by High pod
resource "google_sql_database_instance" "private" {
  name             = "${var.cluster_name}-private-db"
  database_version = "POSTGRES_15"
  region           = var.region
  
  # Deletion protection based on variable
  deletion_protection = var.database_deletion_protection
  
  settings {
    tier = var.database_tier
    
    # Network configuration - private IP only
    ip_configuration {
      ipv4_enabled    = false
      private_network = google_compute_network.vpc.id
    }
    
    # Basic backup configuration
    backup_configuration {
      enabled    = true
      start_time = "03:00"
    }
    
    # Database flags
    database_flags {
      name  = "max_connections"
      value = "50"
    }
  }
  
  depends_on = [google_service_networking_connection.private_vpc_connection]
}

# Private database
resource "google_sql_database" "private_syftbox" {
  name     = "syftbox_private"
  instance = google_sql_database_instance.private.name
}

# Private database user
resource "google_sql_user" "private_syftbox" {
  name     = "syftbox"
  instance = google_sql_database_instance.private.name
  password = random_password.private_db_password.result
}

# Mock database resources (for testing/public data)
resource "random_password" "mock_db_password" {
  length  = 16
  special = true
}

resource "google_sql_database_instance" "mock" {
  count            = var.enable_mock_database ? 1 : 0
  name             = "${var.cluster_name}-mock-db"
  database_version = "POSTGRES_15"
  region           = var.region
  
  # Deletion protection based on variable
  deletion_protection = var.database_deletion_protection
  
  settings {
    tier = var.database_tier
    
    # Network configuration - both private and public IP
    ip_configuration {
      ipv4_enabled    = true
      private_network = google_compute_network.vpc.id
      
      # Allow connections from external VMs
      authorized_networks {
        name  = "allow-all"
        value = "0.0.0.0/0"
      }
    }
    
    # Basic backup configuration
    backup_configuration {
      enabled    = true
      start_time = "04:00"
    }
    
    # Database flags
    database_flags {
      name  = "max_connections"
      value = "100"
    }
  }
  
  depends_on = [google_service_networking_connection.private_vpc_connection]
}

# Mock database (only if enabled)
resource "google_sql_database" "mock_syftbox" {
  count    = var.enable_mock_database ? 1 : 0
  name     = "syftbox_mock"
  instance = google_sql_database_instance.mock[0].name
}

# Mock database user (only if enabled)
resource "google_sql_user" "mock_syftbox" {
  count    = var.enable_mock_database ? 1 : 0
  name     = "syftbox_mock"
  instance = google_sql_database_instance.mock[0].name
  password = random_password.mock_db_password.result
}