terraform {
  required_providers {
    pxgrid = {
      source  = "bastet-cat/pxgrid"
      version = "~> 0.1"
    }
  }
}

provider "pxgrid" {
  endpoint = var.endpoint
  insecure = var.insecure

  api_token = var.api_token

  ssh_username                  = var.ssh_username
  ssh_private_key_file          = var.ssh_private_key_file
  ssh_host                      = var.ssh_host
  ssh_strict_host_key_checking  = var.ssh_strict_host_key_checking
  ssh_known_hosts_file          = var.ssh_known_hosts_file
}
