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

func TestAccTemplateDownload(t *testing.T) {
	ts := newMockTemplateServerWithExisting(false)
	defer ts.Close()

	cfg := fmt.Sprintf(`
provider "pxgrid" {
  endpoint = "%s"
  insecure = true
  username = "root@pam"
  password = "secret"
}

resource "pxgrid_template" "tpl" {
  node_name  = "pve"
  storage    = "local"
  file_name  = "debian-12.tar.zst"
  source_url = "http://example.test/debian-12.tar.zst"
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
					resource.TestCheckResourceAttr("pxgrid_template.tpl", "id", "local:vztmpl/debian-12.tar.zst"),
					resource.TestCheckResourceAttr("pxgrid_template.tpl", "verify_tls", "true"),
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

func TestAccTemplateData(t *testing.T) {
	ts := newMockTemplateServerWithExisting(true)
	defer ts.Close()

	cfg := fmt.Sprintf(`
provider "pxgrid" {
  endpoint = "%s"
  insecure = true
  username = "root@pam"
  password = "secret"
}

data "pxgrid_template" "tpl" {
  node_name = "pve"
  storage   = "local"
  file_name = "debian-12.tar.zst"
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
					resource.TestCheckResourceAttr("data.pxgrid_template.tpl", "id", "local:vztmpl/debian-12.tar.zst"),
				),
			},
		},
	})
}

func TestAccTemplateDataMissing(t *testing.T) {
	ts := newMockTemplateServerWithExisting(false)
	defer ts.Close()

	cfg := fmt.Sprintf(`
provider "pxgrid" {
  endpoint = "%s"
  insecure = true
  username = "root@pam"
  password = "secret"
}

data "pxgrid_template" "tpl" {
  node_name = "pve"
  storage   = "local"
  file_name = "debian-12.tar.zst"
}
`, ts.URL)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"pxgrid": providerserver.NewProtocol6WithError(New()),
		},
		Steps: []resource.TestStep{
			{
				Config:      cfg,
				ExpectError: regexp.MustCompile("Template não encontrado"),
			},
		},
	})
}

func newMockTemplateServerWithExisting(existing bool) *httptest.Server {
	var mu sync.Mutex
	templateExists := existing

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.URL.Path == "/api2/json/access/ticket" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"ticket":"TICKET","CSRFPreventionToken":"CSRF"}}`))
		case r.URL.Path == "/api2/json/nodes/pve/storage/local/content" && r.Method == http.MethodGet:
			if templateExists {
				w.Write([]byte(`{"data":[{"volid":"local:vztmpl/debian-12.tar.zst","content":"vztmpl"}]}`))
			} else {
				w.Write([]byte(`{"data":[]}`))
			}
		case r.URL.Path == "/api2/json/nodes/pve/storage/local/download-url" && r.Method == http.MethodPost:
			if err := r.ParseForm(); err == nil {
				if strings.HasSuffix(r.FormValue("filename"), "debian-12.tar.zst") {
					templateExists = true
				}
			}
			w.Write([]byte(`{"data":"UPID:pve:download-template"}`))
		case r.URL.Path == "/api2/json/nodes/pve/tasks/UPID%3Apve%3Adownload-template/status" && r.Method == http.MethodGet:
			w.Write([]byte(`{"data":{"status":"stopped","exitstatus":"OK"}}`))
		case r.URL.Path == "/api2/json/nodes/pve/tasks/UPID%3Apve%3Adownload-template/log" && r.Method == http.MethodGet:
			w.Write([]byte(`{"data":[]}`))
		case strings.HasPrefix(r.URL.Path, "/api2/json/nodes/pve/storage/local/content/") && r.Method == http.MethodDelete:
			templateExists = false
			w.Write([]byte(`{"data":null}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
}
