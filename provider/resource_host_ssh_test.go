package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccHostPasswordMissingSSH(t *testing.T) {
	cfg := `
provider "pxgrid" {
  endpoint = "https://example.test:8006/"
  insecure = true
}

resource "pxgrid_host_password" "pwd" {
  node_name = "pve"
  username  = "root"
  password  = "secret"
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"pxgrid": providerserver.NewProtocol6WithError(New()),
		},
		Steps: []resource.TestStep{
			{
				Config:      cfg,
				ExpectError: regexp.MustCompile("(?i)ssh ausente"),
			},
		},
	})
}

func TestAccHostAuthorizedKeyMissingSSH(t *testing.T) {
	cfg := `
provider "pxgrid" {
  endpoint = "https://example.test:8006/"
  insecure = true
}

resource "pxgrid_host_authorized_key" "key" {
  node_name  = "pve"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBASE64TESTKEY"
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"pxgrid": providerserver.NewProtocol6WithError(New()),
		},
		Steps: []resource.TestStep{
			{
				Config:      cfg,
				ExpectError: regexp.MustCompile("(?i)ssh ausente"),
			},
		},
	})
}

func TestAccHostNATMissingSSH(t *testing.T) {
	cfg := `
provider "pxgrid" {
  endpoint = "https://example.test:8006/"
  insecure = true
}

resource "pxgrid_host_nat" "nat" {
  node_name = "pve"
  bridge    = "vmbr1"
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"pxgrid": providerserver.NewProtocol6WithError(New()),
		},
		Steps: []resource.TestStep{
			{
				Config:      cfg,
				ExpectError: regexp.MustCompile("(?i)ssh ausente"),
			},
		},
	})
}

func TestAccHostFirewallMissingSSH(t *testing.T) {
	cfg := `
provider "pxgrid" {
  endpoint = "https://example.test:8006/"
  insecure = true
}

resource "pxgrid_host_firewall" "fw" {
  node_name = "pve"
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"pxgrid": providerserver.NewProtocol6WithError(New()),
		},
		Steps: []resource.TestStep{
			{
				Config:      cfg,
				ExpectError: regexp.MustCompile("(?i)ssh ausente"),
			},
		},
	})
}
