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

resource "pxgrid_container" "app" {
  node_name        = var.node_name
  vmid             = var.vmid
  hostname         = "pxgrid-app"
  template_file_id = var.template_file_id
  datastore_id     = var.datastore_id
  bridge           = var.bridge
  ipv4_address     = var.ipv4_address
  ipv4_gateway     = var.ipv4_gateway

  startup_files = {
    "/root/bootstrap.txt" = "${path.module}/bootstrap.txt"
  }

  startup_script_path = "${path.module}/startup.sh"
}
