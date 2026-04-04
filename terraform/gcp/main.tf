###############################################################################
# GCP — Distributed Vehicle Capacity System
# 3 × e2-medium VMs, VPC, Firewall rules, Network Load Balancer
###############################################################################

terraform {
  required_version = ">= 1.6"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.30"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
  zone    = var.zone
}

##############################################################################
# VPC + Subnet
##############################################################################

resource "google_compute_network" "vcs" {
  name                    = "vcs-vpc"
  auto_create_subnetworks = false
}

resource "google_compute_subnetwork" "vcs" {
  name          = "vcs-subnet"
  ip_cidr_range = "10.0.1.0/24"
  region        = var.region
  network       = google_compute_network.vcs.id
}

##############################################################################
# Firewall Rules
##############################################################################

resource "google_compute_firewall" "allow_ssh" {
  name    = "vcs-allow-ssh"
  network = google_compute_network.vcs.name

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  source_ranges = [var.management_cidr]
  target_tags   = ["vcs-node"]
}

resource "google_compute_firewall" "allow_http" {
  name    = "vcs-allow-http"
  network = google_compute_network.vcs.name

  allow {
    protocol = "tcp"
    ports    = ["80"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["vcs-node"]
}

resource "google_compute_firewall" "allow_internal" {
  name    = "vcs-allow-internal"
  network = google_compute_network.vcs.name

  # Swarm control (2377), overlay gossip (7946 TCP+UDP), VXLAN (4789 UDP), Postgres (5432)
  allow {
    protocol = "tcp"
    ports    = ["2377", "7946", "5432"]
  }

  allow {
    protocol = "udp"
    ports    = ["7946", "4789"]
  }

  source_ranges = ["10.0.1.0/24"]
  target_tags   = ["vcs-node"]
}

##############################################################################
# Static Internal IPs
##############################################################################

resource "google_compute_address" "vm_internal" {
  count        = 3
  name         = "vcs-vm${count.index + 1}-internal"
  address_type = "INTERNAL"
  subnetwork   = google_compute_subnetwork.vcs.id
  address      = "10.0.1.${count.index + 11}"
  region       = var.region
}

##############################################################################
# VMs
##############################################################################

resource "google_compute_instance" "vm" {
  count        = 3
  name         = "vcs-vm${count.index + 1}"
  machine_type = var.machine_type
  zone         = var.zone

  tags = ["vcs-node"]

  labels = {
    project     = "vcs"
    environment = "prototype"
    role        = count.index == 0 ? "manager" : "worker"
  }

  boot_disk {
    initialize_params {
      image = "ubuntu-os-cloud/ubuntu-2204-lts"
      size  = 30
      type  = "pd-balanced"
    }
  }

  network_interface {
    subnetwork = google_compute_subnetwork.vcs.id
    network_ip = google_compute_address.vm_internal[count.index].address

    access_config {
      # Ephemeral external IP — use for SSH access during setup
      # Remove access_config block to make VMs private (behind LB only)
    }
  }

  metadata = {
    ssh-keys       = "${var.admin_username}:${file(var.ssh_public_key_path)}"
    startup-script = templatefile("${path.module}/startup.sh.tpl", {
      is_manager = count.index == 0
      manager_ip = "10.0.1.11"
      vm_index   = count.index + 1
    })
  }
}

##############################################################################
# Network Load Balancer (Layer 4 TCP — port 80)
##############################################################################

resource "google_compute_address" "lb" {
  name   = "vcs-lb-ip"
  region = var.region
}

resource "google_compute_instance_group" "vcs" {
  name      = "vcs-instance-group"
  zone      = var.zone
  instances = [for vm in google_compute_instance.vm : vm.self_link]

  named_port {
    name = "http"
    port = 80
  }
}

resource "google_compute_http_health_check" "vcs" {
  name               = "vcs-health-check"
  request_path       = "/nginx-health"
  port               = 80
  check_interval_sec = 10
  timeout_sec        = 5
}

resource "google_compute_target_pool" "vcs" {
  name          = "vcs-target-pool"
  region        = var.region
  instances     = [for vm in google_compute_instance.vm : vm.self_link]
  health_checks = [google_compute_http_health_check.vcs.self_link]
}

resource "google_compute_forwarding_rule" "http" {
  name                  = "vcs-forwarding-rule"
  region                = var.region
  load_balancing_scheme = "EXTERNAL"
  port_range            = "80"
  target                = google_compute_target_pool.vcs.self_link
  ip_address            = google_compute_address.lb.address
}
