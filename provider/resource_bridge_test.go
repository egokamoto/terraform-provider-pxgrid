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

func TestAccBridgeBasic(t *testing.T) {
	ts := newMockBridgeServer()
	defer ts.Close()

	cfg := fmt.Sprintf(`
provider "pxgrid" {
  endpoint = "%s"
  insecure = true
  username = "root@pam"
  password = "secret"
}

resource "pxgrid_network_bridge" "b" {
  node_name = "pve"
  name      = "vmbr1"
  apply_network = true
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
					resource.TestCheckResourceAttr("pxgrid_network_bridge.b", "name", "vmbr1"),
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

func TestAccBridgeFallbackToInterfaceEndpoint(t *testing.T) {
	ts := newMockBridgeServerWithInterfaceFallback()
	defer ts.Close()

	cfg := fmt.Sprintf(`
provider "pxgrid" {
  endpoint = "%s"
  insecure = true
  username = "root@pam"
  password = "secret"
}

resource "pxgrid_network_bridge" "b" {
  node_name = "pve"
  name      = "vmbr1"
  apply_network = true
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
					resource.TestCheckResourceAttr("pxgrid_network_bridge.b", "name", "vmbr1"),
				),
			},
		},
	})
}

func newMockBridgeServer() *httptest.Server {
	var mu sync.Mutex
	bridgeExists := false
	reloadDone := false

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.URL.Path == "/api2/json/access/ticket" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"ticket":"TICKET","CSRFPreventionToken":"CSRF"}}`))
		case r.URL.Path == "/api2/json/nodes/pve/network" && r.Method == http.MethodPost:
			if err := r.ParseForm(); err == nil {
				if r.FormValue("iface") == "vmbr1" {
					bridgeExists = true
				}
			}
			w.Write([]byte(`{"data":null}`))
		case r.URL.Path == "/api2/json/nodes/pve/network" && r.Method == http.MethodPut:
			reloadDone = true
			w.Write([]byte(`{"data":"UPID:pve:reload-network"}`))
		case r.URL.Path == "/api2/json/nodes/pve/tasks/UPID%3Apve%3Areload-network/status" && r.Method == http.MethodGet:
			if reloadDone {
				w.Write([]byte(`{"data":{"status":"stopped","exitstatus":"OK"}}`))
			} else {
				w.Write([]byte(`{"data":{"status":"running","exitstatus":""}}`))
			}
		case r.URL.Path == "/api2/json/nodes/pve/tasks/UPID%3Apve%3Areload-network/log" && r.Method == http.MethodGet:
			w.Write([]byte(`{"data":[]}`))
		case r.URL.Path == "/api2/json/nodes/pve/network" && r.Method == http.MethodGet:
			if bridgeExists && reloadDone {
				w.Write([]byte(`{"data":[{"iface":"vmbr1","type":"bridge","active":1}]}`))
			} else {
				w.Write([]byte(`{"data":[]}`))
			}
		case strings.HasPrefix(r.URL.Path, "/api2/json/nodes/pve/network/vmbr1") && r.Method == http.MethodDelete:
			bridgeExists = false
			w.Write([]byte(`{"data":null}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
}

func newMockBridgeServerWithInterfaceFallback() *httptest.Server {
	var mu sync.Mutex
	bridgeExists := false
	reloadDone := false

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.URL.Path == "/api2/json/access/ticket" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"ticket":"TICKET","CSRFPreventionToken":"CSRF"}}`))
		case r.URL.Path == "/api2/json/nodes/pve/network" && r.Method == http.MethodPost:
			if err := r.ParseForm(); err == nil && r.FormValue("iface") == "vmbr1" {
				bridgeExists = true
			}
			w.Write([]byte(`{"data":null}`))
		case r.URL.Path == "/api2/json/nodes/pve/network" && r.Method == http.MethodPut:
			reloadDone = true
			w.Write([]byte(`{"data":"UPID:pve:reload-network"}`))
		case r.URL.Path == "/api2/json/nodes/pve/tasks/UPID%3Apve%3Areload-network/status" && r.Method == http.MethodGet:
			if reloadDone {
				w.Write([]byte(`{"data":{"status":"stopped","exitstatus":"OK"}}`))
			} else {
				w.Write([]byte(`{"data":{"status":"running","exitstatus":""}}`))
			}
		case r.URL.Path == "/api2/json/nodes/pve/tasks/UPID%3Apve%3Areload-network/log" && r.Method == http.MethodGet:
			w.Write([]byte(`{"data":[]}`))
		case r.URL.Path == "/api2/json/nodes/pve/network" && r.Method == http.MethodGet:
			w.Write([]byte(`{"data":[]}`))
		case r.URL.Path == "/api2/json/nodes/pve/network/vmbr1" && r.Method == http.MethodGet:
			if bridgeExists && reloadDone {
				w.Write([]byte(`{"data":{"iface":"vmbr1","type":"bridge","active":1}}`))
			} else {
				http.Error(w, "not found", http.StatusNotFound)
			}
		case strings.HasPrefix(r.URL.Path, "/api2/json/nodes/pve/network/vmbr1") && r.Method == http.MethodDelete:
			bridgeExists = false
			w.Write([]byte(`{"data":null}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
}
