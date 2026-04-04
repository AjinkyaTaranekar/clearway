###############################################################################
# AWS — Distributed Vehicle Capacity System
# 3 × t3.small EC2 instances, VPC, Security Groups, Network Load Balancer
###############################################################################

terraform {
  required_version = ">= 1.6"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.50"
    }
  }
}

provider "aws" {
  region = var.region
}

##############################################################################
# VPC + Subnet (single AZ for Swarm overlay — all 3 VMs on same subnet)
##############################################################################

resource "aws_vpc" "vcs" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags = merge(var.tags, { Name = "vcs-vpc" })
}

resource "aws_internet_gateway" "vcs" {
  vpc_id = aws_vpc.vcs.id
  tags   = merge(var.tags, { Name = "vcs-igw" })
}

resource "aws_subnet" "vcs" {
  vpc_id                  = aws_vpc.vcs.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = var.availability_zone
  map_public_ip_on_launch = true
  tags                    = merge(var.tags, { Name = "vcs-subnet" })
}

resource "aws_route_table" "vcs" {
  vpc_id = aws_vpc.vcs.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.vcs.id
  }
  tags = merge(var.tags, { Name = "vcs-rt" })
}

resource "aws_route_table_association" "vcs" {
  subnet_id      = aws_subnet.vcs.id
  route_table_id = aws_route_table.vcs.id
}

##############################################################################
# Security Group
##############################################################################

resource "aws_security_group" "vcs" {
  name        = "vcs-sg"
  description = "VCS node security group"
  vpc_id      = aws_vpc.vcs.id
  tags        = merge(var.tags, { Name = "vcs-sg" })

  # SSH — restrict to management IP in production
  ingress {
    description = "SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = [var.management_cidr]
  }

  # HTTP
  ingress {
    description = "HTTP"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # Docker Swarm control plane
  ingress {
    description = "Swarm management"
    from_port   = 2377
    to_port     = 2377
    protocol    = "tcp"
    cidr_blocks = ["10.0.1.0/24"]
  }

  # Swarm gossip TCP
  ingress {
    description = "Swarm gossip TCP"
    from_port   = 7946
    to_port     = 7946
    protocol    = "tcp"
    cidr_blocks = ["10.0.1.0/24"]
  }

  # Swarm gossip UDP
  ingress {
    description = "Swarm gossip UDP"
    from_port   = 7946
    to_port     = 7946
    protocol    = "udp"
    cidr_blocks = ["10.0.1.0/24"]
  }

  # VXLAN overlay
  ingress {
    description = "VXLAN overlay"
    from_port   = 4789
    to_port     = 4789
    protocol    = "udp"
    cidr_blocks = ["10.0.1.0/24"]
  }

  # PostgreSQL logical replication
  ingress {
    description = "PostgreSQL"
    from_port   = 5432
    to_port     = 5432
    protocol    = "tcp"
    cidr_blocks = ["10.0.1.0/24"]
  }

  # All outbound
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

##############################################################################
# Fixed Private IPs via network interfaces
##############################################################################

resource "aws_network_interface" "vm" {
  count           = 3
  subnet_id       = aws_subnet.vcs.id
  private_ips     = ["10.0.1.${count.index + 11}"]
  security_groups = [aws_security_group.vcs.id]
  tags            = merge(var.tags, { Name = "vcs-vm${count.index + 1}-eni" })
}

##############################################################################
# EC2 Instances
##############################################################################

data "aws_ami" "ubuntu" {
  most_recent = true
  owners      = ["099720109477"] # Canonical

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

resource "aws_instance" "vm" {
  count         = 3
  ami           = data.aws_ami.ubuntu.id
  instance_type = var.instance_type
  key_name      = aws_key_pair.vcs.key_name

  network_interface {
    network_interface_id = aws_network_interface.vm[count.index].id
    device_index         = 0
  }

  root_block_device {
    volume_size           = 30
    volume_type           = "gp3"
    delete_on_termination = true
  }

  user_data = templatefile("${path.module}/user-data.sh.tpl", {
    is_manager = count.index == 0
    manager_ip = "10.0.1.11"
    vm_index   = count.index + 1
  })

  tags = merge(var.tags, {
    Name = "vcs-vm${count.index + 1}"
    Role = count.index == 0 ? "swarm-manager" : "swarm-worker"
  })
}

resource "aws_key_pair" "vcs" {
  key_name   = "vcs-key"
  public_key = file(var.ssh_public_key_path)
  tags       = var.tags
}

##############################################################################
# Elastic IPs (stable public IPs for SSH)
##############################################################################

resource "aws_eip" "vm" {
  count    = 3
  domain   = "vpc"
  instance = aws_instance.vm[count.index].id
  tags     = merge(var.tags, { Name = "vcs-vm${count.index + 1}-eip" })
}

##############################################################################
# Network Load Balancer (Layer 4 TCP — port 80)
##############################################################################

resource "aws_lb" "vcs" {
  name               = "vcs-nlb"
  internal           = false
  load_balancer_type = "network"
  subnets            = [aws_subnet.vcs.id]
  tags               = merge(var.tags, { Name = "vcs-nlb" })
}

resource "aws_lb_target_group" "http" {
  name        = "vcs-http-tg"
  port        = 80
  protocol    = "TCP"
  vpc_id      = aws_vpc.vcs.id
  target_type = "instance"

  health_check {
    protocol            = "HTTP"
    path                = "/nginx-health"
    port                = "80"
    healthy_threshold   = 2
    unhealthy_threshold = 2
    interval            = 10
  }

  tags = var.tags
}

resource "aws_lb_target_group_attachment" "vm" {
  count            = 3
  target_group_arn = aws_lb_target_group.http.arn
  target_id        = aws_instance.vm[count.index].id
  port             = 80
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.vcs.arn
  port              = 80
  protocol          = "TCP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.http.arn
  }
}
