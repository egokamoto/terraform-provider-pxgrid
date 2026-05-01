package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccContainerBasic(t *testing.T) {
	ts := newMockContainerServer()
	defer ts.Close()

	cfg := fmt.Sprintf(`
provider "pxgrid" {
  endpoint = "%s"
  insecure = true
  username = "root@pam"
  password = "secret"
}

resource "pxgrid_container" "c" {
  vmid             = 300
  node_name        = "pve"
  hostname         = "tf-acc-lxc"
  template_file_id = "local:vztmpl/debian-12.tar.gz"
  cores            = 1
  memory           = 256
  disk_size        = 4
  bridge           = "vmbr0"
  ipv4_address     = "192.0.2.10/24"
  ipv4_gateway     = "192.0.2.1"
}
`, ts.URL)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"pxgrid": providerserver.NewProtocol6WithError(New()),
		},
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("pxgrid_container.c", "vmid", "300"),
					resource.TestCheckResourceAttr("pxgrid_container.c", "hostname", "tf-acc-lxc"),
				),
			},
			{
				Config:             cfg,
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
		},
	})
}

func TestAccContainerMissing500(t *testing.T) {
	ts, setExists := newMockContainerServerWithMissingStatus(http.StatusInternalServerError)
	defer ts.Close()

	cfg := fmt.Sprintf(`
provider "pxgrid" {
  endpoint = "%s"
  insecure = true
  username = "root@pam"
  password = "secret"
}

resource "pxgrid_container" "c" {
  vmid             = 300
  node_name        = "pve"
  hostname         = "tf-acc-lxc"
  template_file_id = "local:vztmpl/debian-12.tar.gz"
  cores            = 1
  memory           = 256
  disk_size        = 4
  bridge           = "vmbr0"
  ipv4_address     = "192.0.2.10/24"
  ipv4_gateway     = "192.0.2.1"
}
`, ts.URL)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"pxgrid": providerserver.NewProtocol6WithError(New()),
		},
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("pxgrid_container.c", "vmid", "300"),
					resource.TestCheckResourceAttr("pxgrid_container.c", "hostname", "tf-acc-lxc"),
				),
			},
			{
				PreConfig: func() {
					setExists(false)
				},
				Config:             cfg,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func newMockContainerServer() *httptest.Server {
	srv, _ := newMockContainerServerWithMissingStatus(http.StatusNotFound)
	return srv
}

func newMockContainerServerWithMissingStatus(missingStatus int) (*httptest.Server, func(bool)) {
	var mu sync.Mutex
	containerCreated := false
	bridgeExists := false

	setExists := func(v bool) {
		mu.Lock()
		containerCreated = v
		mu.Unlock()
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.URL.Path == "/api2/json/access/ticket" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"ticket":"TICKET","CSRFPreventionToken":"CSRF"}}`))
		case r.URL.Path == "/api2/json/nodes/pve/network" && r.Method == http.MethodGet:
			if bridgeExists {
				w.Write([]byte(`{"data":[{"iface":"vnet1","type":"bridge"}]}`))
				return
			}
			w.Write([]byte(`{"data":[]}`))
		case r.URL.Path == "/api2/json/cluster/sdn" && r.Method == http.MethodPut:
			bridgeExists = true
			w.Write([]byte(`{"data":null}`))
		case r.URL.Path == "/api2/json/nodes/pve/lxc" && r.Method == http.MethodPost:
			containerCreated = true
			w.Write([]byte(`{"data":null}`))
		case r.URL.Path == "/api2/json/nodes/pve/lxc/300/config" && r.Method == http.MethodGet:
			if !containerCreated {
				writeMissingContainer(w, missingStatus)
				return
			}
			w.Write([]byte(`{"data":{"hostname":"tf-acc-lxc"}}`))
		case strings.HasPrefix(r.URL.Path, "/api2/json/nodes/pve/lxc/300") && r.Method == http.MethodDelete:
			containerCreated = false
			w.Write([]byte(`{"data":null}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))

	return srv, setExists
}

func writeMissingContainer(w http.ResponseWriter, status int) {
	if status == http.StatusInternalServerError {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(`{"message":"Configuration file 'nodes/pve/lxc/300.conf' does not exist\n","data":null}`))
		return
	}
	http.Error(w, "not found", status)
}
