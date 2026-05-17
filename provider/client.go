package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Client realiza chamadas REST na API do Proxmox.
// Implementação mínima para usuários, tokens e ACLs.
type Client struct {
	Endpoint   string
	Insecure   bool
	APIToken   string
	Username   string
	Password   string
	HTTPClient *http.Client

	csrfToken string
	ticket    string

	SSHUser string
	SSHKey  string
	SSHHost string
	SSHPass string

	SSHStrictHostKeyChecking bool
	SSHKnownHostsFile        string
}

type apiResponse struct {
	Data json.RawMessage `json:"data"`
}

type taskStatusResponse struct {
	Status     string `json:"status"`
	ExitStatus string `json:"exitstatus"`
}

type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("api error status %d: %s", e.Status, e.Body)
}

func NewClient(endpoint string, insecure bool, apiToken, username, password string) *Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure}, //nolint:gosec
	}
	return &Client{
		Endpoint: strings.TrimRight(endpoint, "/"),
		Insecure: insecure,
		APIToken: apiToken,
		Username: username,
		Password: password,
		HTTPClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: tr,
		},
	}
}

func (c *Client) WithSSH(user, key, host, password string) *Client {
	c.SSHUser = user
	c.SSHKey = key
	c.SSHHost = host
	c.SSHPass = password
	return c
}

func (c *Client) ensureAuth(ctx context.Context) error {
	if c.APIToken != "" || c.ticket != "" {
		return nil
	}
	if c.Username == "" || c.Password == "" {
		return fmt.Errorf("missing credentials")
	}
	form := url.Values{}
	form.Set("username", c.Username)
	form.Set("password", c.Password)

	resp, err := c.do(ctx, http.MethodPost, "/access/ticket", strings.NewReader(form.Encode()), func(r *http.Request) {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	})
	if err != nil {
		return err
	}
	var data struct {
		CSRFPreventionToken string `json:"CSRFPreventionToken"`
		Ticket              string `json:"ticket"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return fmt.Errorf("decode ticket: %w", err)
	}
	if data.Ticket == "" {
		return fmt.Errorf("empty ticket in response")
	}
	c.ticket = data.Ticket
	c.csrfToken = data.CSRFPreventionToken
	return nil
}

func (c *Client) RunSSHCommand(ctx context.Context, host string, cmd []string) error {
	if c.SSHUser == "" || c.SSHKey == "" {
		return fmt.Errorf("ssh credentials not configured")
	}
	keyFile, err := os.CreateTemp("", "pxgrid-ssh-key-*")
	if err != nil {
		return err
	}
	defer os.Remove(keyFile.Name())
	if _, err := keyFile.WriteString(c.SSHKey); err != nil {
		return err
	}
	keyFile.Chmod(0600)
	keyFile.Close()

	sshCmd := []string{"ssh"}
	if c.SSHStrictHostKeyChecking {
		knownHosts, err := c.resolveKnownHostsPath()
		if err != nil {
			return err
		}
		sshCmd = append(sshCmd, "-o", "StrictHostKeyChecking=yes")
		if knownHosts != "" {
			sshCmd = append(sshCmd, "-o", fmt.Sprintf("UserKnownHostsFile=%s", knownHosts))
		}
	} else {
		sshCmd = append(sshCmd, "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null")
	}
	sshCmd = append(sshCmd,
		"-o", "BatchMode=yes",
		"-o", "PreferredAuthentications=publickey",
		"-o", "PasswordAuthentication=no",
		"-o", "KbdInteractiveAuthentication=no",
		"-o", "ConnectTimeout=10",
	)
	sshCmd = append(sshCmd, "-i", keyFile.Name(), fmt.Sprintf("%s@%s", c.SSHUser, host))
	if len(cmd) > 0 {
		escaped := make([]string, 0, len(cmd))
		for _, part := range cmd {
			escaped = append(escaped, shellEscape(part))
		}
		sshCmd = append(sshCmd, strings.Join(escaped, " "))
	}
	var stdout strings.Builder
	var stderr strings.Builder
	command := exec.CommandContext(ctx, sshCmd[0], sshCmd[1:]...)
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("ssh command failed: %w (stdout: %s stderr: %s)", err, strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (c *Client) RunSSHCommandOutput(ctx context.Context, host string, cmd []string) (string, error) {
	if c.SSHUser == "" || c.SSHKey == "" {
		return "", fmt.Errorf("ssh credentials not configured")
	}
	keyFile, err := os.CreateTemp("", "pxgrid-ssh-key-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(keyFile.Name())
	if _, err := keyFile.WriteString(c.SSHKey); err != nil {
		return "", err
	}
	keyFile.Chmod(0600)
	keyFile.Close()

	sshCmd := []string{"ssh"}
	if c.SSHStrictHostKeyChecking {
		knownHosts, err := c.resolveKnownHostsPath()
		if err != nil {
			return "", err
		}
		sshCmd = append(sshCmd, "-o", "StrictHostKeyChecking=yes")
		if knownHosts != "" {
			sshCmd = append(sshCmd, "-o", fmt.Sprintf("UserKnownHostsFile=%s", knownHosts))
		}
	} else {
		sshCmd = append(sshCmd, "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null")
	}
	sshCmd = append(sshCmd,
		"-o", "BatchMode=yes",
		"-o", "PreferredAuthentications=publickey",
		"-o", "PasswordAuthentication=no",
		"-o", "KbdInteractiveAuthentication=no",
		"-o", "ConnectTimeout=10",
	)
	sshCmd = append(sshCmd, "-i", keyFile.Name(), fmt.Sprintf("%s@%s", c.SSHUser, host))
	if len(cmd) > 0 {
		escaped := make([]string, 0, len(cmd))
		for _, part := range cmd {
			escaped = append(escaped, shellEscape(part))
		}
		sshCmd = append(sshCmd, strings.Join(escaped, " "))
	}
	var stdout strings.Builder
	var stderr strings.Builder
	command := exec.CommandContext(ctx, sshCmd[0], sshCmd[1:]...)
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("ssh command failed: %w (stdout: %s stderr: %s)", err, strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func (c *Client) RunSSHCommandPassword(ctx context.Context, host, user, password, script string) error {
	if user == "" || password == "" {
		return fmt.Errorf("ssh password credentials ausentes")
	}
	addr := net.JoinHostPort(host, "22")
	var hostKeyCallback ssh.HostKeyCallback
	if c.SSHStrictHostKeyChecking {
		knownHosts, err := c.resolveKnownHostsPath()
		if err != nil {
			return err
		}
		callback, err := knownhosts.New(knownHosts)
		if err != nil {
			return fmt.Errorf("known_hosts invalido: %w", err)
		}
		hostKeyCallback = callback
	} else {
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	}
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: hostKeyCallback,
		Timeout:         30 * time.Second,
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial ssh: %w", err)
	}
	clientConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		return fmt.Errorf("ssh connect: %w", err)
	}
	sshClient := ssh.NewClient(clientConn, chans, reqs)
	defer sshClient.Close()

	session, err := sshClient.NewSession()
	if err != nil {
		return fmt.Errorf("ssh session: %w", err)
	}
	defer session.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	if err := session.Run(script); err != nil {
		return fmt.Errorf("ssh command failed: %w (stdout: %s stderr: %s)", err, strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (c *Client) resolveKnownHostsPath() (string, error) {
	path := c.SSHKnownHostsFile
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve known_hosts: %w", err)
		}
		path = filepath.Join(home, ".ssh", "known_hosts")
	}
	path = filepath.Clean(path)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("known_hosts não encontrado: %s", path)
	}
	return path, nil
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, mutate ...func(*http.Request)) (*apiResponse, error) {
	urlStr := fmt.Sprintf("%s/api2/json%s", c.Endpoint, path)
	req, err := http.NewRequestWithContext(ctx, method, urlStr, body)
	if err != nil {
		return nil, err
	}
	for _, fn := range mutate {
		fn(req)
	}
	if c.APIToken != "" {
		req.Header.Set("Authorization", "PVEAPIToken "+c.APIToken)
	}
	if c.ticket != "" {
		req.AddCookie(&http.Cookie{Name: "PVEAuthCookie", Value: c.ticket})
	}
	if c.csrfToken != "" && (method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete) {
		req.Header.Set("CSRFPreventionToken", c.csrfToken)
	}

	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		return nil, &APIError{Status: res.StatusCode, Body: string(b)}
	}
	var api apiResponse
	if err := json.Unmarshal(b, &api); err != nil {
		return nil, fmt.Errorf("decode api response: %w", err)
	}
	return &api, nil
}

// User operations

type User struct {
	UserID  string   `json:"userid"`
	Comment string   `json:"comment,omitempty"`
	Enable  int      `json:"enable,omitempty"`
	Email   string   `json:"email,omitempty"`
	Groups  []string `json:"groups,omitempty"`
}

func (c *Client) CreateUser(ctx context.Context, user User, password string) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	form := url.Values{}
	form.Set("userid", user.UserID)
	if password != "" {
		form.Set("password", password)
	}
	if user.Comment != "" {
		form.Set("comment", user.Comment)
	}
	if user.Email != "" {
		form.Set("email", user.Email)
	}
	form.Set("enable", fmt.Sprintf("%d", user.Enable))
	if len(user.Groups) > 0 {
		form.Set("groups", strings.Join(user.Groups, ","))
	}

	_, err := c.do(ctx, http.MethodPost, "/access/users", strings.NewReader(form.Encode()), func(r *http.Request) {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	})
	return err
}

func (c *Client) GetUser(ctx context.Context, userID string) (*User, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, http.MethodGet, "/access/users/"+url.PathEscape(userID), nil)
	if err != nil {
		return nil, err
	}
	var u User
	// API pode retornar objeto ou array com único elemento.
	if err := json.Unmarshal(resp.Data, &u); err == nil && u.UserID != "" {
		return &u, nil
	}
	var arr []User
	if err := json.Unmarshal(resp.Data, &arr); err == nil && len(arr) > 0 {
		return &arr[0], nil
	}
	var generic []map[string]interface{}
	if err := json.Unmarshal(resp.Data, &generic); err == nil && len(generic) > 0 {
		if uid, ok := generic[0]["userid"].(string); ok {
			enable := 0
			if v, ok := generic[0]["enable"].(float64); ok {
				if v != 0 {
					enable = 1
				}
			}
			return &User{
				UserID: uid,
				Enable: enable,
			}, nil
		}
	}
	// fallback: assume exists if status 200
	return &User{UserID: userID}, nil
}

func (c *Client) DeleteUser(ctx context.Context, userID string) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodDelete, "/access/users/"+url.PathEscape(userID), nil)
	return err
}

func (c *Client) UpdateUser(ctx context.Context, user User) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	form := url.Values{}
	if user.Comment != "" {
		form.Set("comment", user.Comment)
	}
	if user.Email != "" {
		form.Set("email", user.Email)
	}
	form.Set("enable", fmt.Sprintf("%d", user.Enable))
	if len(user.Groups) > 0 {
		form.Set("groups", strings.Join(user.Groups, ","))
	}
	path := "/access/users/" + url.PathEscape(user.UserID)
	_, err := c.do(ctx, http.MethodPut, path, strings.NewReader(form.Encode()), func(r *http.Request) {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	})
	return err
}

// ACL
func (c *Client) AddACL(ctx context.Context, path, userID, role string, propagate bool) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	prop := 0
	if propagate {
		prop = 1
	}
	form := url.Values{}
	form.Set("path", path)
	form.Set("roles", role)
	form.Set("users", userID)
	form.Set("propagate", fmt.Sprintf("%d", prop))

	_, err := c.do(ctx, http.MethodPut, "/access/acl", strings.NewReader(form.Encode()), func(r *http.Request) {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	})
	return err
}

// Token operations

type Token struct {
	TokenID string          `json:"tokenid"`
	Comment string          `json:"comment,omitempty"`
	Expire  int64           `json:"expire,omitempty"`
	PrivSep json.RawMessage `json:"privsep,omitempty"` // pode vir como bool ou número
	Value   string          `json:"value,omitempty"`
}

func (c *Client) CreateToken(ctx context.Context, userID, tokenName, comment string, expire int64, privsep bool) (*Token, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return nil, err
	}
	form := url.Values{}
	if comment != "" {
		form.Set("comment", comment)
	}
	if expire > 0 {
		form.Set("expire", fmt.Sprintf("%d", expire))
	}
	form.Set("privsep", boolToIntString(privsep))

	path := fmt.Sprintf("/access/users/%s/token/%s", url.PathEscape(userID), url.PathEscape(tokenName))
	resp, err := c.do(ctx, http.MethodPost, path, strings.NewReader(form.Encode()), func(r *http.Request) {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	})
	if err != nil {
		return nil, err
	}
	var tok Token
	if err := json.Unmarshal(resp.Data, &tok); err != nil {
		return nil, fmt.Errorf("decode token: %w", err)
	}
	return &tok, nil
}

func (c *Client) GetToken(ctx context.Context, userID, tokenName string) (*Token, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/access/users/%s/token/%s", url.PathEscape(userID), url.PathEscape(tokenName))
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var tok Token
	if err := json.Unmarshal(resp.Data, &tok); err != nil {
		return nil, fmt.Errorf("decode token: %w", err)
	}
	return &tok, nil
}

func (c *Client) DeleteToken(ctx context.Context, userID, tokenName string) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	path := fmt.Sprintf("/access/users/%s/token/%s", url.PathEscape(userID), url.PathEscape(tokenName))
	_, err := c.do(ctx, http.MethodDelete, path, nil)
	return err
}

func boolToIntString(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// SDN zone VLAN operations

type SDNZoneVLAN struct {
	ID         string   `json:"zone"`
	Bridge     string   `json:"bridge"`
	MTU        int64    `json:"mtu,omitempty"`
	Nodes      []string `json:"nodes,omitempty"`
	DNS        string   `json:"dns,omitempty"`
	DNSZone    string   `json:"dnszone,omitempty"`
	IPAM       string   `json:"ipam,omitempty"`
	ReverseDNS string   `json:"reversedns,omitempty"`
	Type       string   `json:"type"`
	Pending    bool     `json:"pending,omitempty"`
}

func (c *Client) CreateSDNZoneVLAN(ctx context.Context, zone SDNZoneVLAN) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	form := url.Values{}
	form.Set("zone", zone.ID)
	form.Set("type", "vlan")
	form.Set("bridge", zone.Bridge)
	if zone.MTU > 0 {
		form.Set("mtu", fmt.Sprintf("%d", zone.MTU))
	}
	if len(zone.Nodes) > 0 {
		form.Set("nodes", strings.Join(zone.Nodes, ","))
	}
	if zone.DNS != "" {
		form.Set("dns", zone.DNS)
	}
	if zone.DNSZone != "" {
		form.Set("dnszone", zone.DNSZone)
	}
	if zone.IPAM != "" {
		form.Set("ipam", zone.IPAM)
	}
	if zone.ReverseDNS != "" {
		form.Set("reversedns", zone.ReverseDNS)
	}
	_, err := c.do(ctx, http.MethodPost, "/cluster/sdn/zones", strings.NewReader(form.Encode()), func(r *http.Request) {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	})
	return err
}

func (c *Client) UpdateSDNZoneVLAN(ctx context.Context, zone SDNZoneVLAN) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	form := url.Values{}
	form.Set("bridge", zone.Bridge)
	if zone.MTU > 0 {
		form.Set("mtu", fmt.Sprintf("%d", zone.MTU))
	}
	if len(zone.Nodes) > 0 {
		form.Set("nodes", strings.Join(zone.Nodes, ","))
	}
	if zone.DNS != "" {
		form.Set("dns", zone.DNS)
	}
	if zone.DNSZone != "" {
		form.Set("dnszone", zone.DNSZone)
	}
	if zone.IPAM != "" {
		form.Set("ipam", zone.IPAM)
	}
	if zone.ReverseDNS != "" {
		form.Set("reversedns", zone.ReverseDNS)
	}
	path := "/cluster/sdn/zones/" + url.PathEscape(zone.ID)
	_, err := c.do(ctx, http.MethodPut, path, strings.NewReader(form.Encode()), func(r *http.Request) {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	})
	return err
}

func (c *Client) GetSDNZoneVLAN(ctx context.Context, id string) (*SDNZoneVLAN, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, http.MethodGet, "/cluster/sdn/zones/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	var zone SDNZoneVLAN
	if err := json.Unmarshal(resp.Data, &zone); err == nil && zone.ID != "" {
		return &zone, nil
	}
	var arr []SDNZoneVLAN
	if err := json.Unmarshal(resp.Data, &arr); err == nil && len(arr) > 0 {
		zone = arr[0]
		return &zone, nil
	}
	return nil, fmt.Errorf("decode sdn zone: unexpected response")
}

func (c *Client) DeleteSDNZoneVLAN(ctx context.Context, id string) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodDelete, "/cluster/sdn/zones/"+url.PathEscape(id), nil)
	return err
}

func (c *Client) ApplySDNZone(ctx context.Context, id string) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	// Aplicar mudanças pendentes globalmente (Proxmox não expõe apply por zona).
	_, err := c.do(ctx, http.MethodPut, "/cluster/sdn", nil)
	return err
}

// SDN VNet operations

type SDNVNet struct {
	ID           string `json:"vnet"`
	Zone         string `json:"zone"`
	Alias        string `json:"alias,omitempty"`
	Tag          int64  `json:"tag,omitempty"`
	IsolatePorts bool   `json:"isolate_ports,omitempty"`
	VLANAware    bool   `json:"vlanaware,omitempty"`
	Type         string `json:"type,omitempty"`
	Pending      bool   `json:"pending,omitempty"`
}

type StorageContent struct {
	VolumeID string `json:"volid"`
	Format   string `json:"format,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Used     int64  `json:"used,omitempty"`
}

type NetworkInterface struct {
	Iface       string `json:"iface"`
	Type        string `json:"type"`
	BridgePorts string `json:"bridge_ports,omitempty"`
	Active      int    `json:"active,omitempty"`
	Autostart   int    `json:"autostart,omitempty"`
}

type BridgeOptions struct {
	Ports       []string
	Autostart   bool
	IPv4Address string
	IPv4Prefix  int
	IPv4Gateway string
	VLANAware   bool
	BridgeVIDs  []string
}

func prefixToNetmask(prefix int) (string, error) {
	if prefix < 0 || prefix > 32 {
		return "", fmt.Errorf("prefixo IPv4 inválido: %d", prefix)
	}
	var mask uint32
	if prefix == 0 {
		mask = 0
	} else {
		mask = ^uint32(0) << (32 - prefix)
	}
	parts := []string{
		fmt.Sprintf("%d", (mask>>24)&0xFF),
		fmt.Sprintf("%d", (mask>>16)&0xFF),
		fmt.Sprintf("%d", (mask>>8)&0xFF),
		fmt.Sprintf("%d", mask&0xFF),
	}
	return strings.Join(parts, "."), nil
}

func (c *Client) CreateSDNVNet(ctx context.Context, v SDNVNet) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	form := url.Values{}
	form.Set("vnet", v.ID)
	form.Set("zone", v.Zone)
	if v.Alias != "" {
		form.Set("alias", v.Alias)
	}
	if v.Tag > 0 {
		form.Set("tag", fmt.Sprintf("%d", v.Tag))
	}
	if v.IsolatePorts {
		form.Set("isolate_ports", "1")
	}
	if v.VLANAware {
		form.Set("vlanaware", "1")
	}
	_, err := c.do(ctx, http.MethodPost, "/cluster/sdn/vnets", strings.NewReader(form.Encode()), func(r *http.Request) {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	})
	return err
}

func (c *Client) UpdateSDNVNet(ctx context.Context, v SDNVNet) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	form := url.Values{}
	if v.Alias != "" {
		form.Set("alias", v.Alias)
	}
	if v.Tag > 0 {
		form.Set("tag", fmt.Sprintf("%d", v.Tag))
	}
	if v.IsolatePorts {
		form.Set("isolate_ports", "1")
	}
	if v.VLANAware {
		form.Set("vlanaware", "1")
	}
	path := "/cluster/sdn/vnets/" + url.PathEscape(v.ID)
	_, err := c.do(ctx, http.MethodPut, path, strings.NewReader(form.Encode()), func(r *http.Request) {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	})
	return err
}

func (c *Client) GetSDNVNet(ctx context.Context, id string) (*SDNVNet, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, http.MethodGet, "/cluster/sdn/vnets/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	var v SDNVNet
	if err := json.Unmarshal(resp.Data, &v); err == nil && v.ID != "" {
		return &v, nil
	}
	var arr []SDNVNet
	if err := json.Unmarshal(resp.Data, &arr); err == nil && len(arr) > 0 {
		v = arr[0]
		return &v, nil
	}
	return nil, fmt.Errorf("decode sdn vnet: unexpected response")
}

func (c *Client) DeleteSDNVNet(ctx context.Context, id string) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodDelete, "/cluster/sdn/vnets/"+url.PathEscape(id), nil)
	return err
}

func (c *Client) ApplySDNVNet(ctx context.Context, id string) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	// Aplicar mudanças pendentes globalmente (mesmo endpoint usado para zonas).
	_, err := c.do(ctx, http.MethodPut, "/cluster/sdn", nil)
	return err
}

// Template operations

func (c *Client) TemplateExists(ctx context.Context, node, storage, fileName string) (bool, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return false, err
	}
	path := fmt.Sprintf("/nodes/%s/storage/%s/content", url.PathEscape(node), url.PathEscape(storage))
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return false, err
	}
	var contents []StorageContent
	if err := json.Unmarshal(resp.Data, &contents); err != nil {
		return false, fmt.Errorf("decode storage content: %w", err)
	}
	target := fmt.Sprintf("%s:vztmpl/%s", storage, fileName)
	for _, ctn := range contents {
		if ctn.VolumeID == target {
			return true, nil
		}
	}
	return false, nil
}

func (c *Client) DownloadTemplate(ctx context.Context, node, storage, fileName, sourceURL string) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	form := url.Values{}
	form.Set("filename", fileName)
	form.Set("url", sourceURL)
	form.Set("content", "vztmpl")
	path := fmt.Sprintf("/nodes/%s/storage/%s/download-url", url.PathEscape(node), url.PathEscape(storage))
	resp, err := c.do(ctx, http.MethodPost, path, strings.NewReader(form.Encode()), func(r *http.Request) {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	})
	if err != nil {
		return err
	}
	var upid string
	if err := json.Unmarshal(resp.Data, &upid); err == nil && strings.HasPrefix(upid, "UPID:") {
		return c.waitForTask(ctx, node, upid, 10*time.Minute)
	}
	return nil
}

func (c *Client) UploadTemplate(ctx context.Context, node, storage, fileName string, src io.Reader) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)

	go func() {
		defer pw.Close()
		defer mw.Close()
		_ = mw.WriteField("content", "vztmpl")
		_ = mw.WriteField("filename", fileName)
		part, err := mw.CreateFormFile("filename", fileName)
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, src); err != nil {
			pw.CloseWithError(err)
			return
		}
	}()

	urlStr := fmt.Sprintf("%s/api2/json/nodes/%s/storage/%s/upload", c.Endpoint, url.PathEscape(node), url.PathEscape(storage))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, pr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if c.APIToken != "" {
		req.Header.Set("Authorization", "PVEAPIToken "+c.APIToken)
	}
	if c.ticket != "" {
		req.AddCookie(&http.Cookie{Name: "PVEAuthCookie", Value: c.ticket})
	}
	if c.csrfToken != "" {
		req.Header.Set("CSRFPreventionToken", c.csrfToken)
	}

	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode >= 400 {
		return &APIError{Status: res.StatusCode, Body: string(b)}
	}
	var api apiResponse
	if err := json.Unmarshal(b, &api); err != nil {
		return fmt.Errorf("decode upload response: %w", err)
	}
	return nil
}

func (c *Client) FetchAndUploadTemplate(ctx context.Context, node, storage, fileName, sourceURL string, verifySource bool) error {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !verifySource}, //nolint:gosec
	}
	srcClient := &http.Client{Transport: tr, Timeout: 0}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	resp, err := srcClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("download template: status %d", resp.StatusCode)
	}
	return c.UploadTemplate(ctx, node, storage, fileName, resp.Body)
}

// Network helpers

func (c *Client) BridgeExists(ctx context.Context, node, bridge string) (bool, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return false, err
	}
	items, err := c.GetNetworkInterfaces(ctx, node)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if item.Iface == bridge {
			return true, nil
		}
	}
	return c.networkInterfaceExists(ctx, node, bridge)
}

func (c *Client) networkInterfaceExists(ctx context.Context, node, iface string) (bool, error) {
	path := fmt.Sprintf("/nodes/%s/network/%s", url.PathEscape(node), url.PathEscape(iface))
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			if apiErr.Status == http.StatusNotFound {
				return false, nil
			}
			if apiErr.Status == http.StatusBadRequest &&
				(strings.Contains(apiErr.Body, `"iface":"interface does not exist"`) ||
					strings.Contains(apiErr.Body, "interface does not exist")) {
				return false, nil
			}
		}
		return false, err
	}
	var item NetworkInterface
	if err := json.Unmarshal(resp.Data, &item); err == nil {
		if item.Iface == "" || item.Iface == iface {
			return true, nil
		}
	}
	var generic map[string]interface{}
	if err := json.Unmarshal(resp.Data, &generic); err == nil {
		if name, ok := generic["iface"].(string); ok {
			return name == iface, nil
		}
		return true, nil
	}
	return false, fmt.Errorf("decode network interface %s: %s", iface, string(resp.Data))
}

func (c *Client) DeleteTemplate(ctx context.Context, node, storage, fileName string) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	volid := fmt.Sprintf("%s:vztmpl/%s", storage, fileName)
	path := fmt.Sprintf("/nodes/%s/storage/%s/content/%s", url.PathEscape(node), url.PathEscape(storage), url.PathEscape(volid))
	_, err := c.do(ctx, http.MethodDelete, path, nil)
	return err
}

func (c *Client) ReloadNetwork(ctx context.Context, node string) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	path := fmt.Sprintf("/nodes/%s/network", url.PathEscape(node))
	resp, err := c.do(ctx, http.MethodPut, path, nil)
	if err != nil {
		return err
	}
	var upid string
	if err := json.Unmarshal(resp.Data, &upid); err == nil && strings.HasPrefix(upid, "UPID:") {
		return c.waitForTask(ctx, node, upid, 2*time.Minute)
	}
	return nil
}

func (c *Client) waitForTask(ctx context.Context, node, upid string, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		status, err := c.getTaskStatus(waitCtx, node, upid)
		if err != nil {
			return err
		}
		if status.Status == "stopped" {
			if status.ExitStatus == "" || status.ExitStatus == "OK" || strings.HasPrefix(status.ExitStatus, "WARNINGS:") {
				return nil
			}
			taskLog, logErr := c.getTaskLog(waitCtx, node, upid)
			if logErr == nil && taskLog != "" {
				return fmt.Errorf("task %s failed with exitstatus %s: %s", upid, status.ExitStatus, taskLog)
			}
			return fmt.Errorf("task %s failed with exitstatus %s", upid, status.ExitStatus)
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("timeout aguardando task %s finalizar", upid)
		case <-time.After(2 * time.Second):
		}
	}
}

func (c *Client) getTaskStatus(ctx context.Context, node, upid string) (*taskStatusResponse, error) {
	path := fmt.Sprintf("/nodes/%s/tasks/%s/status", url.PathEscape(node), url.PathEscape(upid))
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var status taskStatusResponse
	if err := json.Unmarshal(resp.Data, &status); err != nil {
		return nil, fmt.Errorf("decode task status: %w", err)
	}
	return &status, nil
}

func (c *Client) getTaskLog(ctx context.Context, node, upid string) (string, error) {
	path := fmt.Sprintf("/nodes/%s/tasks/%s/log", url.PathEscape(node), url.PathEscape(upid))
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	var lines []struct {
		N int    `json:"n"`
		T string `json:"t"`
	}
	if err := json.Unmarshal(resp.Data, &lines); err != nil {
		return "", fmt.Errorf("decode task log: %w", err)
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line.T) != "" {
			out = append(out, line.T)
		}
	}
	return strings.Join(out, " | "), nil
}

func (c *Client) CreateBridge(ctx context.Context, node, name string, opts BridgeOptions) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	form := url.Values{}
	form.Set("type", "bridge")
	form.Set("iface", name)
	if len(opts.Ports) > 0 {
		form.Set("bridge_ports", strings.Join(opts.Ports, " "))
	}
	if opts.Autostart {
		form.Set("autostart", "1")
	}
	if opts.VLANAware {
		form.Set("bridge_vlan_aware", "1")
	}
	if len(opts.BridgeVIDs) > 0 {
		form.Set("bridge_vids", strings.Join(opts.BridgeVIDs, ";"))
	}
	if opts.IPv4Address != "" && opts.IPv4Prefix > 0 {
		netmask, err := prefixToNetmask(opts.IPv4Prefix)
		if err != nil {
			return err
		}
		form.Set("address", opts.IPv4Address)
		form.Set("netmask", netmask)
	}
	if opts.IPv4Gateway != "" {
		form.Set("gateway", opts.IPv4Gateway)
	}
	path := fmt.Sprintf("/nodes/%s/network", url.PathEscape(node))
	_, err := c.do(ctx, http.MethodPost, path, strings.NewReader(form.Encode()), func(r *http.Request) {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	})
	return err
}

func (c *Client) DeleteInterface(ctx context.Context, node, name string) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	path := fmt.Sprintf("/nodes/%s/network/%s", url.PathEscape(node), url.PathEscape(name))
	_, err := c.do(ctx, http.MethodDelete, path, nil)
	return err
}

func (c *Client) GetNetworkInterfaces(ctx context.Context, node string) ([]NetworkInterface, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/nodes/%s/network", url.PathEscape(node))
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var items []NetworkInterface
	if err := json.Unmarshal(resp.Data, &items); err != nil {
		return nil, fmt.Errorf("decode network list: %w", err)
	}
	return items, nil
}

// Container operations

type Container struct {
	VMID     int64  `json:"vmid"`
	Node     string `json:"node"`
	Hostname string `json:"hostname"`
	Cores    int64  `json:"cores"`
	Memory   int64  `json:"memory"`
	Swap     int64  `json:"swap"`
}

func (c *Client) CreateContainer(ctx context.Context, node string, params url.Values) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	path := fmt.Sprintf("/nodes/%s/lxc", url.PathEscape(node))
	resp, err := c.do(ctx, http.MethodPost, path, strings.NewReader(params.Encode()), func(r *http.Request) {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	})
	if err != nil {
		return err
	}
	var upid string
	if err := json.Unmarshal(resp.Data, &upid); err == nil && strings.HasPrefix(upid, "UPID:") {
		if err := c.waitForTask(ctx, node, upid, 10*time.Minute); err != nil {
			return err
		}
		vmid, parseErr := strconv.ParseInt(params.Get("vmid"), 10, 64)
		if parseErr == nil {
			if err := c.waitForContainerPresence(ctx, node, vmid, 3*time.Minute); err != nil {
				return fmt.Errorf("container %d nao estabilizou apos task de criacao: %w", vmid, err)
			}
		}
		return nil
	}
	return nil
}

func (c *Client) GetContainer(ctx context.Context, node string, vmid int64) (*Container, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/nodes/%s/lxc/%d/config", url.PathEscape(node), vmid)
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		if apiErr, ok := err.(*APIError); ok && apiErr.Status == http.StatusInternalServerError && strings.Contains(strings.ToLower(apiErr.Body), "does not exist") {
			return nil, &APIError{
				Status: http.StatusNotFound,
				Body:   apiErr.Body,
			}
		}
		return nil, err
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(resp.Data, &cfg); err != nil {
		return nil, fmt.Errorf("decode container: %w", err)
	}
	hostname, _ := cfg["hostname"].(string)
	cores := parseConfigInt64(cfg["cores"])
	memory := parseConfigInt64(cfg["memory"])
	swap := parseConfigInt64(cfg["swap"])
	return &Container{
		VMID:     vmid,
		Node:     node,
		Hostname: hostname,
		Cores:    cores,
		Memory:   memory,
		Swap:     swap,
	}, nil
}

func (c *Client) UpdateContainerConfig(ctx context.Context, node string, vmid int64, form url.Values) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	if len(form) == 0 {
		return nil
	}
	path := fmt.Sprintf("/nodes/%s/lxc/%d/config", url.PathEscape(node), vmid)
	resp, err := c.do(ctx, http.MethodPut, path, strings.NewReader(form.Encode()), func(r *http.Request) {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	})
	if err != nil {
		return err
	}
	return c.waitForOptionalTask(ctx, node, resp, 5*time.Minute)
}

func (c *Client) UpdateContainerSizing(ctx context.Context, node string, vmid int64, cores, memory, swap int64) error {
	form := url.Values{}
	if cores > 0 {
		form.Set("cores", fmt.Sprintf("%d", cores))
	}
	if memory > 0 {
		form.Set("memory", fmt.Sprintf("%d", memory))
	}
	if swap >= 0 {
		form.Set("swap", fmt.Sprintf("%d", swap))
	}
	return c.UpdateContainerConfig(ctx, node, vmid, form)
}

func (c *Client) StartContainer(ctx context.Context, node string, vmid int64) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	path := fmt.Sprintf("/nodes/%s/lxc/%d/status/start", url.PathEscape(node), vmid)
	resp, err := c.do(ctx, http.MethodPost, path, nil)
	if err != nil {
		return err
	}
	return c.waitForOptionalTask(ctx, node, resp, 5*time.Minute)
}

func parseConfigInt64(v interface{}) int64 {
	switch value := v.(type) {
	case nil:
		return 0
	case int:
		return int64(value)
	case int32:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err == nil {
			return parsed
		}
		return 0
	default:
		return 0
	}
}

func (c *Client) DeleteContainer(ctx context.Context, node string, vmid int64) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	path := fmt.Sprintf("/nodes/%s/lxc/%d", url.PathEscape(node), vmid)
	resp, err := c.do(ctx, http.MethodDelete, path, nil)
	if err == nil {
		if err := c.waitForOptionalTask(ctx, node, resp, 10*time.Minute); err != nil {
			return err
		}
		if err := c.waitForContainerAbsence(ctx, node, vmid, 3*time.Minute); err != nil {
			return fmt.Errorf("container %d ainda nao foi removido por completo apos destroy: %w", vmid, err)
		}
		return nil
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return nil
	}
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusInternalServerError && strings.Contains(strings.ToLower(apiErr.Body), "container is running") {
		if stopErr := c.StopContainer(ctx, node, vmid); stopErr != nil {
			return fmt.Errorf("falha ao parar container %d antes de deletar: %w", vmid, stopErr)
		}
		resp, err = c.do(ctx, http.MethodDelete, path, nil)
		if err == nil {
			if err := c.waitForOptionalTask(ctx, node, resp, 10*time.Minute); err != nil {
				return err
			}
			if err := c.waitForContainerAbsence(ctx, node, vmid, 3*time.Minute); err != nil {
				return fmt.Errorf("container %d ainda nao foi removido por completo apos destroy: %w", vmid, err)
			}
			return nil
		}
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			return nil
		}
	}
	return err
}

func (c *Client) StopContainer(ctx context.Context, node string, vmid int64) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	path := fmt.Sprintf("/nodes/%s/lxc/%d/status/stop", url.PathEscape(node), vmid)
	resp, err := c.do(ctx, http.MethodPost, path, nil)
	if err != nil {
		return err
	}
	var upid string
	if err := json.Unmarshal(resp.Data, &upid); err == nil && strings.HasPrefix(upid, "UPID:") {
		return c.waitForTask(ctx, node, upid, 5*time.Minute)
	}
	return nil
}

func (c *Client) ListContainers(ctx context.Context, node string) ([]Container, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/nodes/%s/lxc", url.PathEscape(node))
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var containers []Container
	if err := json.Unmarshal(resp.Data, &containers); err != nil {
		return nil, fmt.Errorf("decode container list: %w", err)
	}
	return containers, nil
}

func (c *Client) waitForOptionalTask(ctx context.Context, node string, resp *apiResponse, timeout time.Duration) error {
	if resp == nil {
		return nil
	}
	var upid string
	if err := json.Unmarshal(resp.Data, &upid); err == nil && strings.HasPrefix(upid, "UPID:") {
		return c.waitForTask(ctx, node, upid, timeout)
	}
	return nil
}

func (c *Client) waitForContainerPresence(ctx context.Context, node string, vmid int64, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for {
		if err := c.confirmContainerPresence(waitCtx, node, vmid); err == nil {
			return nil
		} else {
			lastErr = err
		}

		select {
		case <-waitCtx.Done():
			if lastErr != nil {
				return lastErr
			}
			return fmt.Errorf("timeout aguardando container %d ficar disponivel", vmid)
		case <-time.After(2 * time.Second):
		}
	}
}

func (c *Client) waitForContainerAbsence(ctx context.Context, node string, vmid int64, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for {
		if err := c.confirmContainerAbsence(waitCtx, node, vmid); err == nil {
			return nil
		} else {
			lastErr = err
		}

		select {
		case <-waitCtx.Done():
			if lastErr != nil {
				return lastErr
			}
			return fmt.Errorf("timeout aguardando container %d desaparecer", vmid)
		case <-time.After(2 * time.Second):
		}
	}
}

func (c *Client) confirmContainerPresence(ctx context.Context, node string, vmid int64) error {
	if _, err := c.GetContainer(ctx, node, vmid); err != nil {
		return fmt.Errorf("container %d ainda nao disponivel na API: %w", vmid, err)
	}
	containers, err := c.ListContainers(ctx, node)
	if err != nil {
		return fmt.Errorf("falha ao listar containers apos create do %d: %w", vmid, err)
	}
	if !containerListHasVMID(containers, vmid) {
		return fmt.Errorf("container %d ainda nao apareceu na listagem de LXCs", vmid)
	}
	if err := c.confirmContainerConfigPresenceOnHost(ctx, node, vmid); err != nil {
		return err
	}
	return nil
}

func (c *Client) confirmContainerAbsence(ctx context.Context, node string, vmid int64) error {
	_, getErr := c.GetContainer(ctx, node, vmid)
	if getErr == nil {
		return fmt.Errorf("container %d ainda existe na API de config", vmid)
	}
	var apiErr *APIError
	if !errors.As(getErr, &apiErr) || apiErr.Status != http.StatusNotFound {
		return fmt.Errorf("falha ao confirmar ausencia do container %d na API: %w", vmid, getErr)
	}
	containers, err := c.ListContainers(ctx, node)
	if err != nil {
		return fmt.Errorf("falha ao listar containers apos destroy do %d: %w", vmid, err)
	}
	if containerListHasVMID(containers, vmid) {
		return fmt.Errorf("container %d ainda aparece na listagem de LXCs", vmid)
	}
	if err := c.confirmContainerConfigAbsenceOnHost(ctx, node, vmid); err != nil {
		return err
	}
	return nil
}

func containerListHasVMID(containers []Container, vmid int64) bool {
	for _, container := range containers {
		if container.VMID == vmid {
			return true
		}
	}
	return false
}

func (c *Client) confirmContainerConfigPresenceOnHost(ctx context.Context, node string, vmid int64) error {
	if c == nil || c.SSHUser == "" || c.SSHKey == "" {
		return nil
	}
	sshHost := node
	if c.SSHHost != "" {
		sshHost = c.SSHHost
	}
	checkPath := fmt.Sprintf("test -f /etc/pve/nodes/%s/lxc/%d.conf", shellEscape(node), vmid)
	if err := c.RunSSHCommand(ctx, sshHost, []string{"bash", "-lc", checkPath}); err != nil {
		return fmt.Errorf("container %d existe na API, mas o config ainda nao apareceu no host", vmid)
	}
	return nil
}

func (c *Client) confirmContainerConfigAbsenceOnHost(ctx context.Context, node string, vmid int64) error {
	if c == nil || c.SSHUser == "" || c.SSHKey == "" {
		return nil
	}
	sshHost := node
	if c.SSHHost != "" {
		sshHost = c.SSHHost
	}
	checkPath := fmt.Sprintf("test ! -f /etc/pve/nodes/%s/lxc/%d.conf", shellEscape(node), vmid)
	if err := c.RunSSHCommand(ctx, sshHost, []string{"bash", "-lc", checkPath}); err != nil {
		return fmt.Errorf("container %d ainda possui config no host", vmid)
	}
	return nil
}

// Firewall rules operations

type FirewallRule struct {
	Pos       int    `json:"pos,omitempty"`
	Type      string `json:"type"`
	Action    string `json:"action"`
	Source    string `json:"source,omitempty"`
	Dest      string `json:"dest,omitempty"`
	Interface string `json:"iface,omitempty"`
	Comment   string `json:"comment,omitempty"`
	Enable    int    `json:"enable,omitempty"`
}

func (c *Client) ListNodeFirewallRules(ctx context.Context, node string) ([]FirewallRule, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/nodes/%s/firewall/rules", url.PathEscape(node))
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var rules []FirewallRule
	if err := json.Unmarshal(resp.Data, &rules); err != nil {
		return nil, fmt.Errorf("decode firewall rules: %w", err)
	}
	return rules, nil
}

func (c *Client) ListClusterFirewallRules(ctx context.Context) ([]FirewallRule, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, http.MethodGet, "/cluster/firewall/rules", nil)
	if err != nil {
		return nil, err
	}
	var rules []FirewallRule
	if err := json.Unmarshal(resp.Data, &rules); err != nil {
		return nil, fmt.Errorf("decode firewall rules: %w", err)
	}
	return rules, nil
}

func (c *Client) CreateNodeFirewallRule(ctx context.Context, node string, rule FirewallRule) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	form := url.Values{}
	form.Set("type", rule.Type)
	form.Set("action", rule.Action)
	if rule.Source != "" {
		form.Set("source", rule.Source)
	}
	if rule.Dest != "" {
		form.Set("dest", rule.Dest)
	}
	if rule.Interface != "" {
		form.Set("iface", rule.Interface)
	}
	if rule.Comment != "" {
		form.Set("comment", rule.Comment)
	}
	form.Set("enable", fmt.Sprintf("%d", rule.Enable))
	path := fmt.Sprintf("/nodes/%s/firewall/rules", url.PathEscape(node))
	_, err := c.do(ctx, http.MethodPost, path, strings.NewReader(form.Encode()), func(r *http.Request) {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	})
	return err
}

func (c *Client) CreateClusterFirewallRule(ctx context.Context, rule FirewallRule) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	form := url.Values{}
	form.Set("type", rule.Type)
	form.Set("action", rule.Action)
	if rule.Source != "" {
		form.Set("source", rule.Source)
	}
	if rule.Dest != "" {
		form.Set("dest", rule.Dest)
	}
	if rule.Interface != "" {
		form.Set("iface", rule.Interface)
	}
	if rule.Comment != "" {
		form.Set("comment", rule.Comment)
	}
	form.Set("enable", fmt.Sprintf("%d", rule.Enable))
	_, err := c.do(ctx, http.MethodPost, "/cluster/firewall/rules", strings.NewReader(form.Encode()), func(r *http.Request) {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	})
	return err
}

func (c *Client) DeleteNodeFirewallRule(ctx context.Context, node string, pos int) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	path := fmt.Sprintf("/nodes/%s/firewall/rules/%d", url.PathEscape(node), pos)
	_, err := c.do(ctx, http.MethodDelete, path, nil)
	return err
}

func (c *Client) DeleteClusterFirewallRule(ctx context.Context, pos int) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	path := fmt.Sprintf("/cluster/firewall/rules/%d", pos)
	_, err := c.do(ctx, http.MethodDelete, path, nil)
	return err
}
