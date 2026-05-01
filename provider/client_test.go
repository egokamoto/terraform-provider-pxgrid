package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestClient_UserTokenLifecycle(t *testing.T) {
	ctx := context.Background()
	var gotTicket, gotUser, gotACL, gotToken bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/access/ticket":
			gotTicket = true
			_ = r.ParseForm()
			if r.Form.Get("username") != "root@pam" || r.Form.Get("password") != "secret" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"ticket":"TICKET","CSRFPreventionToken":"CSRF"}}`))
		case "/api2/json/access/users":
			gotUser = true
			if !strings.Contains(r.Header.Get("Cookie"), "PVEAuthCookie=TICKET") {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Write([]byte(`{"data":null}`))
		case "/api2/json/access/acl":
			gotACL = true
			w.Write([]byte(`{"data":null}`))
		default:
			if strings.HasPrefix(r.URL.Path, "/api2/json/access/users/") {
				if strings.Contains(r.URL.Path, "/token/pxgrid-token") {
					gotToken = true
					w.Write([]byte(`{"data":{"tokenid":"codex@pve!pxgrid-token","value":"toksecret"}}`))
					return
				}
				if strings.Contains(r.URL.Path, "codex") {
					w.Write([]byte(`{"data":{"userid":"codex@pve","enable":1,"comment":"Managed","email":"","groups":["deployers"]}}`))
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, true, "", "root@pam", "secret")

	user := User{
		UserID:  "codex@pve",
		Comment: "Managed",
		Enable:  1,
		Groups:  []string{"deployers"},
	}
	if err := client.CreateUser(ctx, user, "pwd"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := client.AddACL(ctx, "/", user.UserID, "Administrator", true); err != nil {
		t.Fatalf("add acl: %v", err)
	}
	tok, err := client.CreateToken(ctx, user.UserID, "pxgrid-token", "comment", 0, false)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if tok.Value != "toksecret" {
		t.Fatalf("unexpected token value: %s", tok.Value)
	}
	u, err := client.GetUser(ctx, user.UserID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if u.UserID != user.UserID {
		t.Fatalf("user mismatch: %s", u.UserID)
	}

	if !(gotTicket && gotUser && gotACL && gotToken) {
		t.Fatalf("missing calls ticket=%v user=%v acl=%v token=%v", gotTicket, gotUser, gotACL, gotToken)
	}
}

func TestClient_GetToken_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api2/json/access/users/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, true, "user@pve!name=secret", "", "")
	_, err := client.GetToken(context.Background(), "user@pve", "missing")
	if err == nil {
		t.Fatalf("expected error")
	}
	var apiErr *APIError
	if !AsAPIError(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Fatalf("expected APIError 404, got %v", err)
	}
}

// AsAPIError permite reuso em testes.
func AsAPIError(err error, target **APIError) bool {
	if err == nil {
		return false
	}
	if apiErr, ok := err.(*APIError); ok {
		*target = apiErr
		return true
	}
	return false
}

func TestClient_BridgeExistsTreatsBadRequestMissingInterfaceAsAbsent(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/access/ticket":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"ticket":"TICKET","CSRFPreventionToken":"CSRF"}}`))
		case "/api2/json/nodes/pve/network":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":[]}`))
		case "/api2/json/nodes/pve/network/vmbr1":
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"errors":{"iface":"interface does not exist"},"data":null}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, true, "", "root@pam", "secret")
	exists, err := client.BridgeExists(ctx, "pve", "vmbr1")
	if err != nil {
		t.Fatalf("bridge exists: %v", err)
	}
	if exists {
		t.Fatalf("expected missing interface to be treated as absent")
	}
}

func TestClient_ReloadNetworkUsesBarePutAndReturnsTaskLogOnFailure(t *testing.T) {
	ctx := context.Background()
	var sawReload bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/access/ticket":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"ticket":"TICKET","CSRFPreventionToken":"CSRF"}}`))
		case "/api2/json/nodes/pve/network":
			if r.Method != http.MethodPut {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			sawReload = true
			if r.URL.Query().Get("apply") != "" {
				t.Fatalf("reload must not send apply query parameter")
			}
			if err := r.ParseForm(); err == nil && r.Form.Get("apply") != "" {
				t.Fatalf("reload must not send apply form field")
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":"UPID:pve:reload-network"}`))
		case "/api2/json/nodes/pve/tasks/UPID%3Apve%3Areload-network/status", "/api2/json/nodes/pve/tasks/UPID:pve:reload-network/status":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"status":"stopped","exitstatus":"ERROR"}}`))
		case "/api2/json/nodes/pve/tasks/UPID%3Apve%3Areload-network/log", "/api2/json/nodes/pve/tasks/UPID:pve:reload-network/log":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":[{"n":1,"t":"ifreload failed"}]}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, true, "", "root@pam", "secret")
	err := client.ReloadNetwork(ctx, "pve")
	if err == nil {
		t.Fatalf("expected reload task failure")
	}
	if !strings.Contains(err.Error(), "ifreload failed") {
		t.Fatalf("expected task log in error, got %v", err)
	}
	if !sawReload {
		t.Fatalf("reload endpoint was not called")
	}
}

func TestClient_DownloadTemplateSurfacesDownloadURLPermissionError(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/access/ticket":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"ticket":"TICKET","CSRFPreventionToken":"CSRF"}}`))
		case "/api2/json/nodes/pve/storage/local/download-url":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"errors":{"permission":"Datastore.AllocateTemplate required"},"data":null}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, true, "", "root@pam", "secret")
	err := client.DownloadTemplate(ctx, "pve", "local", "debian-12.tar.zst", "http://example.test/debian-12.tar.zst")
	if err == nil {
		t.Fatalf("expected download-url permission error")
	}
	var apiErr *APIError
	if !AsAPIError(err, &apiErr) || apiErr.Status != http.StatusForbidden {
		t.Fatalf("expected APIError 403, got %v", err)
	}
	if !strings.Contains(apiErr.Body, "Datastore.AllocateTemplate") {
		t.Fatalf("expected permission detail in API error body, got %s", apiErr.Body)
	}
}

func TestClient_DownloadTemplateWaitsForReturnedTask(t *testing.T) {
	ctx := context.Background()
	var taskStatusChecked bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/access/ticket":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"ticket":"TICKET","CSRFPreventionToken":"CSRF"}}`))
		case "/api2/json/nodes/pve/storage/local/download-url":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":"UPID:pve:download-template"}`))
		case "/api2/json/nodes/pve/tasks/UPID%3Apve%3Adownload-template/status", "/api2/json/nodes/pve/tasks/UPID:pve:download-template/status":
			taskStatusChecked = true
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"status":"stopped","exitstatus":"OK"}}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, true, "", "root@pam", "secret")
	if err := client.DownloadTemplate(ctx, "pve", "local", "debian-12.tar.zst", "http://example.test/debian-12.tar.zst"); err != nil {
		t.Fatalf("download template: %v", err)
	}
	if !taskStatusChecked {
		t.Fatalf("expected download task status to be checked")
	}
}

func TestWaitForBridgeAbsenceWaitsUntilInterfaceDisappears(t *testing.T) {
	ctx := context.Background()
	var networkChecks int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/access/ticket":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"ticket":"TICKET","CSRFPreventionToken":"CSRF"}}`))
		case "/api2/json/nodes/pve/network":
			networkChecks++
			w.Header().Set("Content-Type", "application/json")
			if networkChecks == 1 {
				w.Write([]byte(`{"data":[{"iface":"vmbr1","type":"bridge","active":1}]}`))
				return
			}
			w.Write([]byte(`{"data":[]}`))
		case "/api2/json/nodes/pve/network/vmbr1":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"data":null}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, true, "", "root@pam", "secret")
	if err := waitForBridgeAbsence(ctx, client, "pve", "vmbr1", false); err != nil {
		t.Fatalf("wait bridge absence: %v", err)
	}
	if networkChecks < 2 {
		t.Fatalf("expected wait to re-check bridge absence, got %d checks", networkChecks)
	}
}

func TestClient_CreateContainerWaitsForPresenceConfirmation(t *testing.T) {
	ctx := context.Background()
	var gotTaskStatus, gotConfig, gotList bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/access/ticket":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"ticket":"TICKET","CSRFPreventionToken":"CSRF"}}`))
		case "/api2/json/nodes/pve/lxc":
			if r.Method == http.MethodPost {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"data":"UPID:create-300"}`))
				return
			}
			gotList = true
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":[{"vmid":300,"node":"pve","hostname":"ct-300"}]}`))
		case "/api2/json/nodes/pve/tasks/UPID:create-300/status":
			gotTaskStatus = true
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"status":"stopped","exitstatus":"OK"}}`))
		case "/api2/json/nodes/pve/lxc/300/config":
			gotConfig = true
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"hostname":"ct-300"}}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, true, "", "root@pam", "secret")
	params := url.Values{}
	params.Set("vmid", "300")
	params.Set("hostname", "ct-300")

	if err := client.CreateContainer(ctx, "pve", params); err != nil {
		t.Fatalf("create container: %v", err)
	}
	if !(gotTaskStatus && gotConfig && gotList) {
		t.Fatalf("missing confirmation calls task=%v config=%v list=%v", gotTaskStatus, gotConfig, gotList)
	}
}

func TestClient_DeleteContainerWaitsForTaskAndAbsenceConfirmation(t *testing.T) {
	ctx := context.Background()
	var gotTaskStatus, gotConfig, gotList bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/access/ticket":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"ticket":"TICKET","CSRFPreventionToken":"CSRF"}}`))
		case "/api2/json/nodes/pve/lxc/300":
			if r.Method == http.MethodDelete {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"data":"UPID:delete-300"}`))
				return
			}
			http.Error(w, "not found", http.StatusNotFound)
		case "/api2/json/nodes/pve/tasks/UPID:delete-300/status":
			gotTaskStatus = true
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"status":"stopped","exitstatus":"OK"}}`))
		case "/api2/json/nodes/pve/lxc/300/config":
			gotConfig = true
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"message":"Configuration file 'nodes/pve/lxc/300.conf' does not exist\n","data":null}`))
		case "/api2/json/nodes/pve/lxc":
			gotList = true
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":[]}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, true, "", "root@pam", "secret")
	if err := client.DeleteContainer(ctx, "pve", 300); err != nil {
		t.Fatalf("delete container: %v", err)
	}
	if !(gotTaskStatus && gotConfig && gotList) {
		t.Fatalf("missing confirmation calls task=%v config=%v list=%v", gotTaskStatus, gotConfig, gotList)
	}
}

func TestClient_ConfirmContainerPresenceRequiresListEntry(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/access/ticket":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"ticket":"TICKET","CSRFPreventionToken":"CSRF"}}`))
		case "/api2/json/nodes/pve/lxc/300/config":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"hostname":"ct-300"}}`))
		case "/api2/json/nodes/pve/lxc":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":[]}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, true, "", "root@pam", "secret")
	err := client.confirmContainerPresence(ctx, "pve", 300)
	if err == nil || !strings.Contains(err.Error(), "listagem de LXCs") {
		t.Fatalf("expected list confirmation error, got %v", err)
	}
}

func TestClient_ConfirmContainerAbsenceRequiresListEntryGone(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/access/ticket":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"ticket":"TICKET","CSRFPreventionToken":"CSRF"}}`))
		case "/api2/json/nodes/pve/lxc/300/config":
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"message":"Configuration file 'nodes/pve/lxc/300.conf' does not exist\n","data":null}`))
		case "/api2/json/nodes/pve/lxc":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":[{"vmid":300,"node":"pve","hostname":"ct-300"}]}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, true, "", "root@pam", "secret")
	err := client.confirmContainerAbsence(ctx, "pve", 300)
	if err == nil || !strings.Contains(err.Error(), "ainda aparece na listagem de LXCs") {
		t.Fatalf("expected list absence error, got %v", err)
	}
}
