variable "endpoint" {
  type = string
}

variable "insecure" {
  type    = bool
  default = false
}

variable "api_token" {
  type      = string
  sensitive = true
}

variable "ssh_username" {
  type = string
}

variable "ssh_private_key_file" {
  type      = string
  sensitive = true
}

variable "ssh_host" {
  type    = string
  default = null
}

variable "node_name" {
  type = string
}

variable "vmid" {
  type    = number
  default = 300
}

variable "template_file_id" {
  type = string
}

variable "datastore_id" {
  type    = string
  default = "local-lvm"
}

variable "bridge" {
  type    = string
  default = "vmbr0"
}

variable "ipv4_address" {
  type = string
}

variable "ipv4_gateway" {
  type = string
}
