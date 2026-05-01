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

func TestAccSDNZoneAndVNet(t *testing.T) {
	ts := newMockSDNServer()
	defer ts.Close()

	cfg := fmt.Sprintf(`
provider "pxgrid" {
  endpoint = "%s"
  insecure = true
  username = "root@pam"
  password = "secret"
}

resource "pxgrid_sdn_zone_vlan" "z" {
  id     = "zone100"
  bridge = "vmbr100"
  mtu    = 1500
}

resource "pxgrid_sdn_vnet" "v" {
  id   = "vnet100"
  zone = pxgrid_sdn_zone_vlan.z.id
  tag  = 100
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
					resource.TestCheckResourceAttr("pxgrid_sdn_zone_vlan.z", "id", "zone100"),
					resource.TestCheckResourceAttr("pxgrid_sdn_vnet.v", "zone", "zone100"),
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

func newMockSDNServer() *httptest.Server {
	var mu sync.Mutex
	zoneCreated := false
	vnetCreated := false

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.URL.Path == "/api2/json/access/ticket" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"ticket":"TICKET","CSRFPreventionToken":"CSRF"}}`))
		case r.URL.Path == "/api2/json/cluster/sdn/zones" && r.Method == http.MethodPost:
			zoneCreated = true
			w.Write([]byte(`{"data":null}`))
		case strings.HasPrefix(r.URL.Path, "/api2/json/cluster/sdn/zones/") && r.Method == http.MethodGet:
			if !zoneCreated {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Write([]byte(`{"data":{"zone":"zone100","bridge":"vmbr100","type":"vlan"}}`))
		case r.URL.Path == "/api2/json/cluster/sdn" && r.Method == http.MethodPut:
			w.Write([]byte(`{"data":null}`))
		case r.URL.Path == "/api2/json/cluster/sdn/vnets" && r.Method == http.MethodPost:
			vnetCreated = true
			w.Write([]byte(`{"data":null}`))
		case strings.HasPrefix(r.URL.Path, "/api2/json/cluster/sdn/vnets/") && r.Method == http.MethodGet:
			if !vnetCreated {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Write([]byte(`{"data":{"vnet":"vnet100","zone":"zone100","tag":100}}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
}

func TestAccSDNZoneAlreadyExists(t *testing.T) {
	ts, setExists := newMockSDNServerWithExisting()
	defer ts.Close()
	setExists(true, false)

	cfg := fmt.Sprintf(`
provider "pxgrid" {
  endpoint = "%s"
  insecure = true
  username = "root@pam"
  password = "secret"
}

resource "pxgrid_sdn_zone_vlan" "z" {
  id     = "zone100"
  bridge = "vmbr100"
  mtu    = 1500
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
					resource.TestCheckResourceAttr("pxgrid_sdn_zone_vlan.z", "id", "zone100"),
					resource.TestCheckResourceAttr("pxgrid_sdn_zone_vlan.z", "bridge", "vmbr100"),
					resource.TestCheckResourceAttr("pxgrid_sdn_zone_vlan.z", "mtu", "1500"),
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

func TestAccSDNZoneUpdate(t *testing.T) {
	ts, setExists := newMockSDNServerWithExisting()
	defer ts.Close()

	cfg := func(mtu int) string {
		return fmt.Sprintf(`
provider "pxgrid" {
  endpoint = "%s"
  insecure = true
  username = "root@pam"
  password = "secret"
}

resource "pxgrid_sdn_zone_vlan" "z" {
  id     = "zone100"
  bridge = "vmbr100"
  mtu    = %d
}
`, ts.URL, mtu)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"pxgrid": providerserver.NewProtocol6WithError(New()),
		},
		Steps: []resource.TestStep{
			{
				Config: cfg(1500),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("pxgrid_sdn_zone_vlan.z", "mtu", "1500"),
				),
			},
			{
				PreConfig: func() { setExists(true, false) },
				Config:    cfg(1600),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("pxgrid_sdn_zone_vlan.z", "mtu", "1600"),
				),
			},
			{
				Config:             cfg(1600),
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
		},
	})
}

func TestAccSDNVNetAlreadyExists(t *testing.T) {
	ts, setExists := newMockSDNServerWithExisting()
	defer ts.Close()
	setExists(true, true)

	cfg := fmt.Sprintf(`
provider "pxgrid" {
  endpoint = "%s"
  insecure = true
  username = "root@pam"
  password = "secret"
}

resource "pxgrid_sdn_zone_vlan" "z" {
  id     = "zone100"
  bridge = "vmbr100"
  mtu    = 1500
}

resource "pxgrid_sdn_vnet" "v" {
  id   = "vnet100"
  zone = pxgrid_sdn_zone_vlan.z.id
  tag  = 100
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
					resource.TestCheckResourceAttr("pxgrid_sdn_vnet.v", "id", "vnet100"),
					resource.TestCheckResourceAttr("pxgrid_sdn_vnet.v", "zone", "zone100"),
					resource.TestCheckResourceAttr("pxgrid_sdn_vnet.v", "tag", "100"),
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

func newMockSDNServerWithExisting() (*httptest.Server, func(bool, bool)) {
	var mu sync.Mutex
	zoneExists := false
	mtu := int64(1500)
	vnetExists := false
	vnetTag := int64(100)

	setExists := func(zone, vnet bool) {
		mu.Lock()
		zoneExists = zone
		vnetExists = vnet
		mu.Unlock()
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.URL.Path == "/api2/json/access/ticket" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"ticket":"TICKET","CSRFPreventionToken":"CSRF"}}`))
		case r.URL.Path == "/api2/json/cluster/sdn/zones" && r.Method == http.MethodPost:
			if zoneExists {
				writeZoneExists(w)
				return
			}
			zoneExists = true
			w.Write([]byte(`{"data":null}`))
		case strings.HasPrefix(r.URL.Path, "/api2/json/cluster/sdn/zones/") && r.Method == http.MethodGet:
			if !zoneExists {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Write([]byte(fmt.Sprintf(`{"data":{"zone":"zone100","bridge":"vmbr100","mtu":%d,"type":"vlan"}}`, mtu)))
		case strings.HasPrefix(r.URL.Path, "/api2/json/cluster/sdn/zones/") && r.Method == http.MethodPut:
			if !zoneExists {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if err := r.ParseForm(); err == nil {
				if v := r.FormValue("mtu"); v != "" {
					fmt.Sscanf(v, "%d", &mtu)
				}
			}
			w.Write([]byte(`{"data":null}`))
		case r.URL.Path == "/api2/json/cluster/sdn" && r.Method == http.MethodPut:
			w.Write([]byte(`{"data":null}`))
		case r.URL.Path == "/api2/json/cluster/sdn/vnets" && r.Method == http.MethodPost:
			if vnetExists {
				writeVNetExists(w)
				return
			}
			vnetExists = true
			w.Write([]byte(`{"data":null}`))
		case strings.HasPrefix(r.URL.Path, "/api2/json/cluster/sdn/vnets/") && r.Method == http.MethodGet:
			if !vnetExists {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Write([]byte(fmt.Sprintf(`{"data":{"vnet":"vnet100","zone":"zone100","tag":%d}}`, vnetTag)))
		case strings.HasPrefix(r.URL.Path, "/api2/json/cluster/sdn/vnets/") && r.Method == http.MethodPut:
			if !vnetExists {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if err := r.ParseForm(); err == nil {
				if v := r.FormValue("tag"); v != "" {
					fmt.Sscanf(v, "%d", &vnetTag)
				}
			}
			w.Write([]byte(`{"data":null}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))

	return srv, setExists
}

func writeZoneExists(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte(`{"message":"create sdn zone object failed: sdn zone object ID 'zone100' already defined\n","data":null}`))
}

func writeVNetExists(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte(`{"message":"create sdn vnet object failed: sdn vnet object ID 'vnet100' already defined\n","data":null}`))
}
