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

var testAccProtoV6ProviderFactories = map[string]func() (any, error){
	"pxgrid": func() (any, error) {
		return providerserver.NewProtocol6WithError(New()), nil
	},
}

func TestAccUserAndTokenPlanApply(t *testing.T) {
	ts := newMockAPIServerAcc()
	defer ts.Close()

	cfg := fmt.Sprintf(`
provider "pxgrid" {
  endpoint  = "%s"
  insecure  = true
  username  = "root@pam"
  password  = "secret"
}

resource "pxgrid_user" "test" {
  user_id   = "tf-acc@pve"
  password  = "acc-pass"
  comment   = "acc user"
  acl_path  = "/"
  acl_role  = "Administrator"
}

resource "pxgrid_user_token" "test" {
  user_id    = pxgrid_user.test.user_id
  token_name = "acc-token"
  comment    = "acc token"
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
					resource.TestCheckResourceAttr("pxgrid_user.test", "user_id", "tf-acc@pve"),
					resource.TestCheckResourceAttr("pxgrid_user_token.test", "user_id", "tf-acc@pve"),
					resource.TestCheckResourceAttr("pxgrid_user_token.test", "token_name", "acc-token"),
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

// newMockAPIServerAcc simula endpoints da API Proxmox usados pelos recursos de user/token para acceptance tests.
func newMockAPIServerAcc() *httptest.Server {
	var mu sync.Mutex
	userCreated := false
	tokenValue := "token-acc-value"

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.URL.Path == "/api2/json/access/ticket" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"ticket":"TICKET","CSRFPreventionToken":"CSRF"}}`))
		case r.URL.Path == "/api2/json/access/users" && r.Method == http.MethodPost:
			userCreated = true
			w.Write([]byte(`{"data":null}`))
		case strings.HasPrefix(r.URL.Path, "/api2/json/access/users/") && r.Method == http.MethodGet:
			if !userCreated {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Write([]byte(`{"data":[{"userid":"tf-acc@pve","enable":1,"comment":"acc user"}]}`))
		case r.URL.Path == "/api2/json/access/acl" && r.Method == http.MethodPut:
			w.Write([]byte(`{"data":null}`))
		case strings.Contains(r.URL.Path, "/token/") && r.Method == http.MethodPost:
			userCreated = true
			w.Write([]byte(`{"data":{"tokenid":"tf-acc@pve!acc-token","value":"` + tokenValue + `","privsep":0}}`))
		case strings.Contains(r.URL.Path, "/token/") && r.Method == http.MethodGet:
			w.Write([]byte(`{"data":{"tokenid":"tf-acc@pve!acc-token","privsep":0}}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
}
