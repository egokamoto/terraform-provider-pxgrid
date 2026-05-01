variable "endpoint" {
  type        = string
  description = "Proxmox API endpoint, for example https://192.168.15.2:8006/."
}

variable "insecure" {
  type        = bool
  description = "Disable TLS validation for the Proxmox endpoint."
  default     = false
}

variable "api_token" {
  type        = string
  description = "Proxmox API token in user@realm!name=secret format."
  sensitive   = true
}

variable "ssh_username" {
  type        = string
  description = "SSH user for host-level resources."
  default     = null
}

variable "ssh_private_key_file" {
  type        = string
  description = "Path to the SSH private key for host-level resources."
  sensitive   = true
  default     = null
}

variable "ssh_host" {
  type        = string
  description = "SSH host override. Defaults to the resource node name when unset."
  default     = null
}

variable "ssh_strict_host_key_checking" {
  type        = bool
  description = "Enable strict SSH host key checking."
  default     = true
}

variable "ssh_known_hosts_file" {
  type        = string
  description = "Path to known_hosts used when strict host key checking is enabled."
  default     = null
}
