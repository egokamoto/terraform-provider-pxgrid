package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
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

func TestAccContainerDetectsDrift(t *testing.T) {
	ts := newMockContainerServerWithConfig(2, 1024, 0)
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
					resource.TestCheckResourceAttr("pxgrid_container.c", "memory", "1024"),
					resource.TestCheckResourceAttr("pxgrid_container.c", "cores", "2"),
					resource.TestCheckResourceAttr("pxgrid_container.c", "swap", "0"),
				),
			},
			{
				Config:             cfg,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestAccContainerUpdatesSizingInPlace(t *testing.T) {
	ts := newMockContainerServerWithConfig(1, 256, 512)
	defer ts.Close()

	cfg := func(cores, memory, swap int64) string {
		return fmt.Sprintf(`
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
  cores            = %d
  memory           = %d
  swap             = %d
  disk_size        = 4
  bridge           = "vmbr0"
  ipv4_address     = "192.0.2.10/24"
  ipv4_gateway     = "192.0.2.1"
}
`, ts.URL, cores, memory, swap)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"pxgrid": providerserver.NewProtocol6WithError(New()),
		},
		Steps: []resource.TestStep{
			{
				Config: cfg(1, 256, 512),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("pxgrid_container.c", "cores", "1"),
					resource.TestCheckResourceAttr("pxgrid_container.c", "memory", "256"),
					resource.TestCheckResourceAttr("pxgrid_container.c", "swap", "512"),
				),
			},
			{
				Config: cfg(2, 1024, 0),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("pxgrid_container.c", "cores", "2"),
					resource.TestCheckResourceAttr("pxgrid_container.c", "memory", "1024"),
					resource.TestCheckResourceAttr("pxgrid_container.c", "swap", "0"),
				),
			},
		},
	})
}

func TestAccContainerUpdatesMutableConfigInPlace(t *testing.T) {
	ts := newMockContainerServerWithConfig(1, 256, 512)
	defer ts.Close()

	cfg := func(hostname string, started bool) string {
		return fmt.Sprintf(`
provider "pxgrid" {
  endpoint = "%s"
  insecure = true
  username = "root@pam"
  password = "secret"
}

resource "pxgrid_container" "c" {
  vmid             = 300
  node_name        = "pve"
  hostname         = "%s"
  template_file_id = "local:vztmpl/debian-12.tar.gz"
  cores            = 1
  memory           = 256
  swap             = 512
  disk_size        = 4
  bridge           = "vmbr0"
  ipv4_address     = "192.0.2.10/24"
  ipv4_gateway     = "192.0.2.1"
  dns_servers      = ["1.1.1.1", "8.8.8.8"]
  tags             = ["dev", "ollama"]
  startup_order    = 20
  start_on_boot    = true
  started          = %t
  nesting          = true
  ssh_public_key   = "ssh-ed25519 AAAATEST test@example"
}
`, ts.URL, hostname, started)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"pxgrid": providerserver.NewProtocol6WithError(New()),
		},
		Steps: []resource.TestStep{
			{
				Config: cfg("tf-acc-lxc", true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("pxgrid_container.c", "hostname", "tf-acc-lxc"),
				),
			},
			{
				Config: cfg("tf-acc-lxc-renamed", false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("pxgrid_container.c", "id", "300"),
					resource.TestCheckResourceAttr("pxgrid_container.c", "hostname", "tf-acc-lxc-renamed"),
					resource.TestCheckResourceAttr("pxgrid_container.c", "started", "false"),
				),
			},
		},
	})
}

func TestAccContainerNet0ChangeFailsClearly(t *testing.T) {
	ts := newMockContainerServerWithConfig(1, 256, 512)
	defer ts.Close()

	cfg := func(ip string) string {
		return fmt.Sprintf(`
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
  swap             = 512
  disk_size        = 4
  bridge           = "vmbr0"
  ipv4_address     = "%s"
  ipv4_gateway     = "192.0.2.1"
}
`, ts.URL, ip)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"pxgrid": providerserver.NewProtocol6WithError(New()),
		},
		Steps: []resource.TestStep{
			{
				Config: cfg("192.0.2.10/24"),
			},
			{
				Config:      cfg("192.0.2.11/24"),
				ExpectError: regexp.MustCompile("Reconstrução de net0 não implementada"),
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

func newMockContainerServerWithConfig(cores, memory, swap int64) *httptest.Server {
	srv, _ := newMockContainerServerWithConfigAndMissingStatus(cores, memory, swap, http.StatusNotFound)
	return srv
}

func newMockContainerServerWithMissingStatus(missingStatus int) (*httptest.Server, func(bool)) {
	return newMockContainerServerWithConfigAndMissingStatus(1, 256, 0, missingStatus)
}

func newMockContainerServerWithConfigAndMissingStatus(cores, memory, swap int64, missingStatus int) (*httptest.Server, func(bool)) {
	var mu sync.Mutex
	containerCreated := false
	bridgeExists := false
	currentHostname := "tf-acc-lxc"
	currentCores := cores
	currentMemory := memory
	currentSwap := swap

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
		case r.URL.Path == "/api2/json/nodes/pve/lxc/300/config" && r.Method == http.MethodPut:
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if v := r.Form.Get("cores"); v != "" {
				fmt.Sscanf(v, "%d", &currentCores)
			}
			if v := r.Form.Get("hostname"); v != "" {
				currentHostname = v
			}
			if v := r.Form.Get("memory"); v != "" {
				fmt.Sscanf(v, "%d", &currentMemory)
			}
			if v := r.Form.Get("swap"); v != "" {
				fmt.Sscanf(v, "%d", &currentSwap)
			}
			w.Write([]byte(`{"data":null}`))
		case r.URL.Path == "/api2/json/nodes/pve/lxc/300/status/start" && r.Method == http.MethodPost:
			w.Write([]byte(`{"data":null}`))
		case r.URL.Path == "/api2/json/nodes/pve/lxc/300/status/stop" && r.Method == http.MethodPost:
			w.Write([]byte(`{"data":null}`))
		case r.URL.Path == "/api2/json/nodes/pve/lxc/300/config" && r.Method == http.MethodGet:
			if !containerCreated {
				writeMissingContainer(w, missingStatus)
				return
			}
			w.Write([]byte(fmt.Sprintf(`{"data":{"hostname":%q,"cores":%d,"memory":%d,"swap":%d}}`, currentHostname, currentCores, currentMemory, currentSwap)))
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
