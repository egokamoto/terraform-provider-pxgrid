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

variable "bridge_name" {
  type    = string
  default = "vmbr10"
}

variable "bridge_ip" {
  type = string
}

variable "bridge_cidr" {
  type    = number
  default = 24
}

variable "source_cidr" {
  type = string
}

variable "outbound_interface" {
  type = string
}
