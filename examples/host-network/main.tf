terraform {
  required_providers {
    pxgrid = {
      source  = "bastet-cat/pxgrid"
      version = "~> 0.1"
    }
  }
}

provider "pxgrid" {
  endpoint  = var.endpoint
  insecure  = var.insecure
  api_token = var.api_token

  ssh_username         = var.ssh_username
  ssh_private_key_file = var.ssh_private_key_file
  ssh_host             = var.ssh_host
}

resource "pxgrid_network_bridge" "isolated" {
  node_name     = var.node_name
  name          = var.bridge_name
  ipv4_address  = var.bridge_ip
  ipv4_cidr     = var.bridge_cidr
  apply_network = true
}

resource "pxgrid_host_nat" "isolated" {
  node_name          = var.node_name
  bridge             = pxgrid_network_bridge.isolated.name
  source_cidr        = var.source_cidr
  outbound_interface = var.outbound_interface

  depends_on = [pxgrid_network_bridge.isolated]
}
