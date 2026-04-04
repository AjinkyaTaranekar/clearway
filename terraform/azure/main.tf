###############################################################################
# Azure — Distributed Vehicle Capacity System
# 3 × Standard_B2s VMs, VNet, NSG, Load Balancer, Public IPs
###############################################################################

terraform {
  required_version = ">= 1.6"
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 3.110"
    }
  }
}

provider "azurerm" {
  features {}
  subscription_id = var.subscription_id
}

##############################################################################
# Resource Group
##############################################################################

resource "azurerm_resource_group" "vcs" {
  name     = var.resource_group_name
  location = var.location
  tags     = var.tags
}

##############################################################################
# Networking
##############################################################################

resource "azurerm_virtual_network" "vcs" {
  name                = "vcs-vnet"
  address_space       = ["10.0.0.0/16"]
  location            = azurerm_resource_group.vcs.location
  resource_group_name = azurerm_resource_group.vcs.name
  tags                = var.tags
}

resource "azurerm_subnet" "vcs" {
  name                 = "vcs-subnet"
  resource_group_name  = azurerm_resource_group.vcs.name
  virtual_network_name = azurerm_virtual_network.vcs.name
  address_prefixes     = ["10.0.1.0/24"]
}

resource "azurerm_network_security_group" "vcs" {
  name                = "vcs-nsg"
  location            = azurerm_resource_group.vcs.location
  resource_group_name = azurerm_resource_group.vcs.name
  tags                = var.tags

  # SSH — restrict to your management IP in production
  security_rule {
    name                       = "allow-ssh"
    priority                   = 100
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "22"
    source_address_prefix      = var.management_cidr
    destination_address_prefix = "*"
  }

  # HTTP — nginx on port 80
  security_rule {
    name                       = "allow-http"
    priority                   = 110
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "80"
    source_address_prefix      = "*"
    destination_address_prefix = "*"
  }

  # Docker Swarm control plane (manager ↔ worker, internal only)
  security_rule {
    name                       = "allow-swarm-mgmt"
    priority                   = 120
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "2377"
    source_address_prefix      = "10.0.1.0/24"
    destination_address_prefix = "*"
  }

  # Docker Swarm overlay network gossip (TCP + UDP)
  security_rule {
    name                       = "allow-swarm-overlay-tcp"
    priority                   = 130
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "7946"
    source_address_prefix      = "10.0.1.0/24"
    destination_address_prefix = "*"
  }

  security_rule {
    name                       = "allow-swarm-overlay-udp"
    priority                   = 131
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Udp"
    source_port_range          = "*"
    destination_port_range     = "7946"
    source_address_prefix      = "10.0.1.0/24"
    destination_address_prefix = "*"
  }

  # Docker overlay VXLAN
  security_rule {
    name                       = "allow-swarm-vxlan"
    priority                   = 132
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Udp"
    source_port_range          = "*"
    destination_port_range     = "4789"
    source_address_prefix      = "10.0.1.0/24"
    destination_address_prefix = "*"
  }

  # PostgreSQL logical replication (internal only)
  security_rule {
    name                       = "allow-postgres"
    priority                   = 140
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "5432"
    source_address_prefix      = "10.0.1.0/24"
    destination_address_prefix = "*"
  }
}

resource "azurerm_subnet_network_security_group_association" "vcs" {
  subnet_id                 = azurerm_subnet.vcs.id
  network_security_group_id = azurerm_network_security_group.vcs.id
}

##############################################################################
# Public IPs for each VM (needed for SSH access + Swarm join token distribution)
##############################################################################

resource "azurerm_public_ip" "vm" {
  count               = 3
  name                = "vcs-vm${count.index + 1}-pip"
  location            = azurerm_resource_group.vcs.location
  resource_group_name = azurerm_resource_group.vcs.name
  allocation_method   = "Static"
  sku                 = "Standard"
  tags                = var.tags
}

##############################################################################
# NICs
##############################################################################

resource "azurerm_network_interface" "vm" {
  count               = 3
  name                = "vcs-vm${count.index + 1}-nic"
  location            = azurerm_resource_group.vcs.location
  resource_group_name = azurerm_resource_group.vcs.name
  tags                = var.tags

  ip_configuration {
    name                          = "internal"
    subnet_id                     = azurerm_subnet.vcs.id
    private_ip_address_allocation = "Static"
    # 10.0.1.11 / .12 / .13
    private_ip_address            = "10.0.1.${count.index + 11}"
    public_ip_address_id          = azurerm_public_ip.vm[count.index].id
  }
}

##############################################################################
# VMs
##############################################################################

resource "azurerm_linux_virtual_machine" "vm" {
  count               = 3
  name                = "vcs-vm${count.index + 1}"
  location            = azurerm_resource_group.vcs.location
  resource_group_name = azurerm_resource_group.vcs.name
  size                = var.vm_size
  admin_username      = var.admin_username
  tags                = var.tags

  network_interface_ids = [azurerm_network_interface.vm[count.index].id]

  admin_ssh_key {
    username   = var.admin_username
    public_key = file(var.ssh_public_key_path)
  }

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Premium_LRS"
    disk_size_gb         = 30
  }

  source_image_reference {
    publisher = "Canonical"
    offer     = "0001-com-ubuntu-server-jammy"
    sku       = "22_04-lts-gen2"
    version   = "latest"
  }

  # Bootstrap: install Docker, join Swarm (vm1 = manager, vm2/3 = workers)
  custom_data = base64encode(templatefile("${path.module}/cloud-init.yaml.tpl", {
    is_manager     = count.index == 0
    manager_ip     = "10.0.1.11"
    vm_index       = count.index + 1
  }))
}

##############################################################################
# Load Balancer — distributes HTTP port 80 across all 3 VMs
##############################################################################

resource "azurerm_public_ip" "lb" {
  name                = "vcs-lb-pip"
  location            = azurerm_resource_group.vcs.location
  resource_group_name = azurerm_resource_group.vcs.name
  allocation_method   = "Static"
  sku                 = "Standard"
  tags                = var.tags
}

resource "azurerm_lb" "vcs" {
  name                = "vcs-lb"
  location            = azurerm_resource_group.vcs.location
  resource_group_name = azurerm_resource_group.vcs.name
  sku                 = "Standard"
  tags                = var.tags

  frontend_ip_configuration {
    name                 = "PublicIPAddress"
    public_ip_address_id = azurerm_public_ip.lb.id
  }
}

resource "azurerm_lb_backend_address_pool" "vcs" {
  name            = "vcs-backend-pool"
  loadbalancer_id = azurerm_lb.vcs.id
}

resource "azurerm_network_interface_backend_address_pool_association" "vm" {
  count                   = 3
  network_interface_id    = azurerm_network_interface.vm[count.index].id
  ip_configuration_name   = "internal"
  backend_address_pool_id = azurerm_lb_backend_address_pool.vcs.id
}

resource "azurerm_lb_probe" "http" {
  name            = "http-probe"
  loadbalancer_id = azurerm_lb.vcs.id
  protocol        = "Http"
  port            = 80
  request_path    = "/nginx-health"
}

resource "azurerm_lb_rule" "http" {
  name                           = "http-rule"
  loadbalancer_id                = azurerm_lb.vcs.id
  protocol                       = "Tcp"
  frontend_port                  = 80
  backend_port                   = 80
  frontend_ip_configuration_name = "PublicIPAddress"
  backend_address_pool_ids       = [azurerm_lb_backend_address_pool.vcs.id]
  probe_id                       = azurerm_lb_probe.http.id
  disable_outbound_snat          = false
}
