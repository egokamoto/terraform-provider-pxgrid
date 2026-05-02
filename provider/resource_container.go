package provider

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &ContainerResource{}
	_ resource.ResourceWithConfigure   = &ContainerResource{}
	_ resource.ResourceWithImportState = &ContainerResource{}
)

type ContainerResource struct {
	client *Client
}

type containerModel struct {
	ID            types.String `tfsdk:"id"`
	VMID          types.Int64  `tfsdk:"vmid"`
	NodeName      types.String `tfsdk:"node_name"`
	Hostname      types.String `tfsdk:"hostname"`
	TemplateFile  types.String `tfsdk:"template_file_id"`
	OSType        types.String `tfsdk:"os_type"`
	Cores         types.Int64  `tfsdk:"cores"`
	Memory        types.Int64  `tfsdk:"memory"`
	Swap          types.Int64  `tfsdk:"swap"`
	DiskSize      types.Int64  `tfsdk:"disk_size"`
	DatastoreID   types.String `tfsdk:"datastore_id"`
	Bridge        types.String `tfsdk:"bridge"`
	VNet          types.String `tfsdk:"vnet"`
	IPv4Address   types.String `tfsdk:"ipv4_address"`
	IPv4Gateway   types.String `tfsdk:"ipv4_gateway"`
	DNSServers    types.List   `tfsdk:"dns_servers"`
	VLANID        types.Int64  `tfsdk:"vlan_id"`
	Tags          types.List   `tfsdk:"tags"`
	StartupOrder  types.Int64  `tfsdk:"startup_order"`
	Unprivileged  types.Bool   `tfsdk:"unprivileged"`
	StartOnBoot   types.Bool   `tfsdk:"start_on_boot"`
	Started       types.Bool   `tfsdk:"started"`
	Nesting       types.Bool   `tfsdk:"nesting"`
	StartupFiles  types.Map    `tfsdk:"startup_files"`
	SSHKey        types.String `tfsdk:"ssh_public_key"`
	StartupScript types.String `tfsdk:"startup_script_path"`
	HostUsername  types.String `tfsdk:"host_username"`
	HostPassword  types.String `tfsdk:"host_password"`
}

func NewContainerResource() resource.Resource {
	return &ContainerResource{}
}

func (r *ContainerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_container"
}

func (r *ContainerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Provisiona LXC no Proxmox.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Identificador interno (vmid).",
			},
			"vmid": schema.Int64Attribute{
				Required:    true,
				Description: "ID do container.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"node_name": schema.StringAttribute{
				Required:    true,
				Description: "Nó alvo.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"hostname": schema.StringAttribute{
				Required:    true,
				Description: "Hostname do container.",
			},
			"template_file_id": schema.StringAttribute{
				Required:    true,
				Description: "Template LXC (ex.: local:vztmpl/debian-12...).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"os_type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Tipo de OS (default debian).",
				Default:     stringdefault.StaticString("debian"),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"cores": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "Cores (default 2).",
				Default:     int64default.StaticInt64(2),
			},
			"memory": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "Memória MB (default 2048).",
				Default:     int64default.StaticInt64(2048),
			},
			"swap": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "Swap MB (default 512).",
				Default:     int64default.StaticInt64(512),
			},
			"disk_size": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "Tamanho do disco GB (default 8).",
				Default:     int64default.StaticInt64(8),
			},
			"datastore_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Datastore alvo (default local-lvm).",
				Default:     stringdefault.StaticString("local-lvm"),
			},
			"bridge": schema.StringAttribute{
				Optional:    true,
				Description: "vmbr a usar (exclusivo com vnet).",
			},
			"vnet": schema.StringAttribute{
				Optional:    true,
				Description: "VNET SDN a usar (exclusivo com bridge).",
			},
			"ipv4_address": schema.StringAttribute{
				Required:    true,
				Description: "Endereço IPv4/CIDR.",
			},
			"ipv4_gateway": schema.StringAttribute{
				Optional:    true,
				Description: "Gateway IPv4.",
			},
			"dns_servers": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "DNS servers para o container.",
			},
			"vlan_id": schema.Int64Attribute{
				Optional:    true,
				Description: "VLAN ID opcional na interface.",
			},
			"tags": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Tags do container.",
			},
			"startup_order": schema.Int64Attribute{
				Optional:    true,
				Description: "Ordem de startup.",
			},
			"unprivileged": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Container não privilegiado (default true).",
				Default:     booldefault.StaticBool(true),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"start_on_boot": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Inicia no boot do host (default true).",
				Default:     booldefault.StaticBool(true),
			},
			"started": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Estado final esperado (default true).",
				Default:     booldefault.StaticBool(true),
			},
			"nesting": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Habilita feature nesting no LXC (default false).",
				Default:     booldefault.StaticBool(false),
			},
			"startup_files": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Mapeia caminho destino no container para caminho local de arquivo a copiar antes do startup script.",
			},
			"ssh_public_key": schema.StringAttribute{
				Optional:    true,
				Description: "Chave pública adicionada ao root do LXC.",
			},
			"startup_script_path": schema.StringAttribute{
				Optional:    true,
				Description: "Script local copiado e executado no container via pct exec.",
			},
			"host_username": schema.StringAttribute{
				Optional:    true,
				Description: "Usuário adicional criado no container para acesso SSH via senha.",
			},
			"host_password": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Senha do host_username (ativa PasswordAuthentication).",
			},
		},
	}
}

func (r *ContainerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan containerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client := r.client
	if client == nil && defaultClient != nil {
		client = defaultClient
	}
	if client == nil {
		resp.Diagnostics.AddError("Provider não configurado", "Client não encontrado.")
		return
	}
	if plan.Bridge.IsNull() && plan.VNet.IsNull() {
		resp.Diagnostics.AddError("Rede obrigatória", "Informe bridge ou vnet.")
		return
	}
	targetBridge := plan.Bridge.ValueString()
	useVNet := targetBridge == ""
	if targetBridge == "" {
		targetBridge = plan.VNet.ValueString()
	}
	if targetBridge == "" {
		resp.Diagnostics.AddError("Rede obrigatória", "Informe bridge ou vnet.")
		return
	}
	var zoneName string
	if !plan.VNet.IsNull() {
		vnet, err := client.GetSDNVNet(ctx, plan.VNet.ValueString())
		if err == nil {
			zoneName = vnet.Zone
			_ = client.ApplySDNVNet(ctx, plan.VNet.ValueString()) // melhor esforço
			if vnet.Zone != "" {
				_ = client.ApplySDNZone(ctx, vnet.Zone) // melhor esforço
			}
		}
	}

	if useVNet {
		if err := waitForVNetReady(ctx, client, plan.VNet.ValueString(), zoneName, plan.NodeName.ValueString()); err != nil {
			resp.Diagnostics.AddError("VNET indisponível", err.Error())
			return
		}
	} else {
		if err := waitForBridgeReady(ctx, client, plan.NodeName.ValueString(), targetBridge, plan.VNet.ValueString(), zoneName); err != nil {
			resp.Diagnostics.AddError("Bridge indisponível", err.Error())
			return
		}
	}
	params := buildLXCParams(plan)
	if err := client.CreateContainer(ctx, plan.NodeName.ValueString(), params); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusInternalServerError && strings.Contains(strings.ToLower(apiErr.Body), "already exists") {
			// allow adoption of existing LXC with same VMID
		} else {
			resp.Diagnostics.AddError("Erro ao criar LXC", err.Error())
			return
		}
	}
	hostUser := ""
	if !plan.HostUsername.IsNull() {
		hostUser = plan.HostUsername.ValueString()
	}
	hostPass := ""
	if !plan.HostPassword.IsNull() {
		hostPass = plan.HostPassword.ValueString()
	}
	if hostUser == "root" && hostPass != "" {
		hostUser = ""
		hostPass = ""
	}
	if hostUser != "" || hostPass != "" {
		if hostUser == "" || hostPass == "" {
			resp.Diagnostics.AddError("Configuração de host incompleta", "Informe host_username e host_password para habilitar acesso SSH por senha.")
			return
		}
		if err := configureContainerHostAccess(ctx, client, plan.NodeName.ValueString(), plan.VMID.ValueInt64(), hostUser, hostPass); err != nil {
			resp.Diagnostics.AddError("Erro ao configurar host", err.Error())
			return
		}
	}
	if err := configureContainerStartupFiles(ctx, client, plan.NodeName.ValueString(), plan.VMID.ValueInt64(), plan.StartupFiles); err != nil {
		resp.Diagnostics.AddError("Erro ao copiar arquivos de bootstrap", err.Error())
		return
	}
	startupScriptPath := ""
	if !plan.StartupScript.IsNull() {
		startupScriptPath = strings.TrimSpace(plan.StartupScript.ValueString())
	}
	if startupScriptPath != "" {
		if !plan.Started.ValueBool() {
			resp.Diagnostics.AddError("Startup script requer container iniciado", "Defina started=true para executar o script.")
			return
		}
		if err := configureContainerStartupScript(ctx, client, plan.NodeName.ValueString(), plan.VMID.ValueInt64(), startupScriptPath); err != nil {
			resp.Diagnostics.AddError("Erro ao executar startup script", err.Error())
			return
		}
	}
	plan.ID = types.StringValue(fmt.Sprintf("%d", plan.VMID.ValueInt64()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ContainerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state containerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client := r.client
	if client == nil && defaultClient != nil {
		client = defaultClient
	}
	if client == nil {
		resp.Diagnostics.AddError("Provider não configurado", "Client não encontrado.")
		return
	}
	vmid := state.VMID.ValueInt64()
	c, err := client.GetContainer(ctx, state.NodeName.ValueString(), vmid)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			if apiErr.Status == http.StatusNotFound || (apiErr.Status == http.StatusInternalServerError && strings.Contains(strings.ToLower(apiErr.Body), "does not exist")) {
				resp.State.RemoveResource(ctx)
				return
			}
		}
		resp.Diagnostics.AddError("Erro ao ler LXC", err.Error())
		return
	}
	state.Hostname = types.StringValue(c.Hostname)
	state.Cores = types.Int64Value(c.Cores)
	state.Memory = types.Int64Value(c.Memory)
	state.Swap = types.Int64Value(c.Swap)
	state.ID = types.StringValue(fmt.Sprintf("%d", vmid))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ContainerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var state containerModel
	var plan containerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if containerNet0Changed(state, plan) {
		resp.Diagnostics.AddError("Reconstrução de net0 não implementada", "Mudanças em bridge, vnet, ipv4_address, ipv4_gateway ou vlan_id exigem reconstrução de net0, que ainda não está implementada.")
		return
	}
	if changed := containerUnsupportedStorageChanged(state, plan); len(changed) > 0 {
		resp.Diagnostics.AddError("Operação não suportada", fmt.Sprintf("Atualização de %s ainda não está implementada.", strings.Join(changed, ", ")))
		return
	}
	client := r.client
	if client == nil && defaultClient != nil {
		client = defaultClient
	}
	if client == nil {
		resp.Diagnostics.AddError("Provider não configurado", "Client não encontrado.")
		return
	}
	vmid := plan.VMID.ValueInt64()
	configForm := buildLXCUpdateConfig(ctx, plan)
	if err := client.UpdateContainerConfig(ctx, plan.NodeName.ValueString(), vmid, configForm); err != nil {
		resp.Diagnostics.AddError("Erro ao atualizar LXC", err.Error())
		return
	}
	if !state.Started.Equal(plan.Started) {
		if plan.Started.ValueBool() {
			if err := client.StartContainer(ctx, plan.NodeName.ValueString(), vmid); err != nil {
				resp.Diagnostics.AddError("Erro ao iniciar LXC", err.Error())
				return
			}
		} else {
			if err := client.StopContainer(ctx, plan.NodeName.ValueString(), vmid); err != nil {
				resp.Diagnostics.AddError("Erro ao parar LXC", err.Error())
				return
			}
		}
	}
	c, err := client.GetContainer(ctx, plan.NodeName.ValueString(), vmid)
	if err != nil {
		resp.Diagnostics.AddError("Erro ao ler LXC", err.Error())
		return
	}
	plan.ID = types.StringValue(fmt.Sprintf("%d", vmid))
	plan.Hostname = types.StringValue(c.Hostname)
	plan.Cores = types.Int64Value(c.Cores)
	plan.Memory = types.Int64Value(c.Memory)
	plan.Swap = types.Int64Value(c.Swap)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func buildLXCUpdateConfig(ctx context.Context, plan containerModel) url.Values {
	form := url.Values{}
	form.Set("hostname", plan.Hostname.ValueString())
	form.Set("cores", fmt.Sprintf("%d", plan.Cores.ValueInt64()))
	form.Set("memory", fmt.Sprintf("%d", plan.Memory.ValueInt64()))
	form.Set("swap", fmt.Sprintf("%d", plan.Swap.ValueInt64()))
	form.Set("onboot", boolToIntString(plan.StartOnBoot.ValueBool()))
	if plan.Nesting.ValueBool() {
		form.Set("features", "nesting=1")
	} else {
		form.Set("features", "nesting=0")
	}
	if !plan.StartupOrder.IsNull() {
		form.Set("startup", fmt.Sprintf("order=%d", plan.StartupOrder.ValueInt64()))
	}
	if !plan.DNSServers.IsNull() && !plan.DNSServers.IsUnknown() {
		var dns []string
		plan.DNSServers.ElementsAs(ctx, &dns, false)
		form.Set("nameserver", strings.Join(dns, " "))
	}
	if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
		var tags []string
		plan.Tags.ElementsAs(ctx, &tags, false)
		form.Set("tags", strings.Join(tags, ";"))
	}
	if !plan.SSHKey.IsNull() {
		form.Set("ssh-public-keys", plan.SSHKey.ValueString())
	}
	return form
}

func containerNet0Changed(state, plan containerModel) bool {
	return !state.Bridge.Equal(plan.Bridge) ||
		!state.VNet.Equal(plan.VNet) ||
		!state.IPv4Address.Equal(plan.IPv4Address) ||
		!state.IPv4Gateway.Equal(plan.IPv4Gateway) ||
		!state.VLANID.Equal(plan.VLANID)
}

func containerUnsupportedStorageChanged(state, plan containerModel) []string {
	var changed []string
	if !state.DiskSize.Equal(plan.DiskSize) {
		changed = append(changed, "disk_size")
	}
	if !state.DatastoreID.Equal(plan.DatastoreID) {
		changed = append(changed, "datastore_id")
	}
	return changed
}

func (r *ContainerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state containerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client := r.client
	if client == nil && defaultClient != nil {
		client = defaultClient
	}
	if client == nil {
		resp.Diagnostics.AddError("Provider não configurado", "Client não encontrado.")
		return
	}
	if err := client.DeleteContainer(ctx, state.NodeName.ValueString(), state.VMID.ValueInt64()); err != nil {
		resp.Diagnostics.AddError("Erro ao deletar LXC", err.Error())
	}
}

func (r *ContainerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		if defaultClient != nil {
			r.client = defaultClient
		}
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError("Provider mal configurado", fmt.Sprintf("Tipo inesperado em ProviderData"))
		return
	}
	r.client = client
}

func (r *ContainerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func buildLXCParams(plan containerModel) url.Values {
	params := url.Values{}
	params.Set("vmid", fmt.Sprintf("%d", plan.VMID.ValueInt64()))
	params.Set("hostname", plan.Hostname.ValueString())
	params.Set("ostemplate", plan.TemplateFile.ValueString())
	params.Set("cores", fmt.Sprintf("%d", plan.Cores.ValueInt64()))
	params.Set("memory", fmt.Sprintf("%d", plan.Memory.ValueInt64()))
	params.Set("swap", fmt.Sprintf("%d", plan.Swap.ValueInt64()))
	params.Set("unprivileged", boolToIntString(plan.Unprivileged.ValueBool()))
	params.Set("start", boolToIntString(plan.Started.ValueBool()))
	if plan.StartOnBoot.ValueBool() {
		params.Set("onboot", "1")
	}
	if plan.Nesting.ValueBool() {
		params.Set("features", "nesting=1")
	}
	disk := fmt.Sprintf("%s:%d", coalesce(plan.DatastoreID.ValueString(), "local-lvm"), plan.DiskSize.ValueInt64())
	params.Set("rootfs", disk)
	// net0
	bridge := plan.Bridge.ValueString()
	if bridge == "" {
		bridge = plan.VNet.ValueString()
	}
	netParts := []string{
		fmt.Sprintf("name=eth0"),
		fmt.Sprintf("bridge=%s", bridge),
	}
	if !plan.VLANID.IsNull() {
		netParts = append(netParts, fmt.Sprintf("tag=%d", plan.VLANID.ValueInt64()))
	}
	netParts = append(netParts, fmt.Sprintf("ip=%s", plan.IPv4Address.ValueString()))
	if !plan.IPv4Gateway.IsNull() {
		netParts = append(netParts, fmt.Sprintf("gw=%s", plan.IPv4Gateway.ValueString()))
	}
	params.Set("net0", strings.Join(netParts, ","))

	// DNS
	if !plan.DNSServers.IsNull() && !plan.DNSServers.IsUnknown() {
		var dns []string
		plan.DNSServers.ElementsAs(context.Background(), &dns, false)
		if len(dns) > 0 {
			params.Set("nameserver", strings.Join(dns, " "))
		}
	}
	// SSH key
	if !plan.SSHKey.IsNull() && plan.SSHKey.ValueString() != "" {
		params.Set("ssh-public-keys", plan.SSHKey.ValueString())
	}
	if !plan.HostUsername.IsNull() && plan.HostUsername.ValueString() == "root" && !plan.HostPassword.IsNull() && plan.HostPassword.ValueString() != "" {
		params.Set("password", plan.HostPassword.ValueString())
	}
	// Startup order
	if !plan.StartupOrder.IsNull() {
		params.Set("startup", fmt.Sprintf("order=%d", plan.StartupOrder.ValueInt64()))
	}
	// Tags
	if !plan.Tags.IsNull() {
		var tags []string
		plan.Tags.ElementsAs(context.Background(), &tags, false)
		if len(tags) > 0 {
			params.Set("tags", strings.Join(tags, ";"))
		}
	}
	return params
}

func coalesce(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func waitForBridgeReady(ctx context.Context, client *Client, node, bridge, vnetID, zoneName string) error {
	for i := 0; i < 150; i++ {
		ok, err := client.BridgeExists(ctx, node, bridge)
		if err != nil {
			return fmt.Errorf("Erro ao verificar bridge: %w", err)
		}
		if ok {
			return nil
		}
		if i%5 == 0 && vnetID != "" {
			_ = client.ApplySDNVNet(ctx, vnetID)
			if zoneName != "" {
				_ = client.ApplySDNZone(ctx, zoneName)
			}
			_ = client.ReloadNetwork(ctx, node)
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("Bridge/VNET %s não encontrada após aguardar.", bridge)
}

func waitForVNetReady(ctx context.Context, client *Client, vnetID, zoneName, node string) error {
	for i := 0; i < 150; i++ {
		vnet, err := client.GetSDNVNet(ctx, vnetID)
		if err != nil {
			return fmt.Errorf("Erro ao verificar vnet %s: %w", vnetID, err)
		}
		if vnet != nil && !vnet.Pending {
			return nil
		}
		if i%5 == 0 {
			_ = client.ApplySDNVNet(ctx, vnetID)
			if zoneName != "" {
				_ = client.ApplySDNZone(ctx, zoneName)
			}
			_ = client.ReloadNetwork(ctx, node)
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("VNET %s não disponível após aguardar aplicação.", vnetID)
}

func configureContainerHostAccess(ctx context.Context, client *Client, node string, vmid int64, username, password string) error {
	if client == nil {
		return fmt.Errorf("Provider não configurado para acesso SSH ao host")
	}
	if err := waitForContainerReady(ctx, client, node, vmid); err != nil {
		return err
	}
	sshHost := node
	if client.SSHHost != "" {
		sshHost = client.SSHHost
	}
	if username != "root" {
		ensureUser := fmt.Sprintf("id -u %s >/dev/null 2>&1 || useradd -m -s /bin/bash %s", shellEscape(username), shellEscape(username))
		if err := runPctExec(ctx, client, sshHost, vmid, ensureUser); err != nil {
			return fmt.Errorf("falha ao criar usuário %s: %w", username, err)
		}
	}
	payload := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s\n", username, password)))
	setPassword := fmt.Sprintf("echo %s | base64 -d | chpasswd", shellEscape(payload))
	if err := runPctExec(ctx, client, sshHost, vmid, setPassword); err != nil {
		return fmt.Errorf("falha ao definir senha para %s: %w", username, err)
	}
	if username == "root" {
		unlockRoot := "passwd -u root >/dev/null 2>&1 || usermod -U root >/dev/null 2>&1 || true"
		_ = runPctExec(ctx, client, sshHost, vmid, unlockRoot)
	}
	enablePassword := "if [ -f /etc/ssh/sshd_config ]; then " +
		"if grep -q '^PasswordAuthentication' /etc/ssh/sshd_config; then sed -i 's/^#\\?PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config; else echo 'PasswordAuthentication yes' >> /etc/ssh/sshd_config; fi;"
	if username == "root" {
		enablePassword += " if grep -q '^PermitRootLogin' /etc/ssh/sshd_config; then sed -i 's/^#\\?PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config; else echo 'PermitRootLogin yes' >> /etc/ssh/sshd_config; fi;"
	}
	enablePassword += " systemctl restart ssh >/dev/null 2>&1 || systemctl restart sshd >/dev/null 2>&1 || service ssh restart >/dev/null 2>&1 || true; fi"
	_ = runPctExec(ctx, client, sshHost, vmid, enablePassword)
	return nil
}

func configureContainerStartupScript(ctx context.Context, client *Client, node string, vmid int64, scriptPath string) error {
	if client == nil {
		return fmt.Errorf("Provider não configurado para startup script")
	}
	if err := waitForContainerReady(ctx, client, node, vmid); err != nil {
		return err
	}
	if scriptPath == "" {
		return fmt.Errorf("startup script vazio")
	}
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		return fmt.Errorf("falha ao ler startup script: %w", err)
	}
	if len(content) == 0 {
		return fmt.Errorf("startup script vazio")
	}
	sshHost := node
	if client.SSHHost != "" {
		sshHost = client.SSHHost
	}
	payload := base64.StdEncoding.EncodeToString(content)
	targetPath := "/root/pxgrid-startup.sh"
	writeScript := fmt.Sprintf("echo %s | base64 -d > %s", shellEscape(payload), shellEscape(targetPath))
	if err := runPctExec(ctx, client, sshHost, vmid, writeScript); err != nil {
		return fmt.Errorf("falha ao copiar startup script: %w", err)
	}
	makeExecutable := fmt.Sprintf("chmod 700 %s", shellEscape(targetPath))
	if err := runPctExec(ctx, client, sshHost, vmid, makeExecutable); err != nil {
		return fmt.Errorf("falha ao preparar startup script: %w", err)
	}
	runScript := fmt.Sprintf("bash %s", shellEscape(targetPath))
	if err := runPctExec(ctx, client, sshHost, vmid, runScript); err != nil {
		return fmt.Errorf("falha ao executar startup script: %w", err)
	}
	return nil
}

func configureContainerStartupFiles(ctx context.Context, client *Client, node string, vmid int64, files types.Map) error {
	if files.IsNull() || files.IsUnknown() {
		return nil
	}
	if client == nil {
		return fmt.Errorf("Provider não configurado para startup files")
	}
	if err := waitForContainerReady(ctx, client, node, vmid); err != nil {
		return err
	}
	var mapping map[string]string
	if diags := files.ElementsAs(ctx, &mapping, false); diags.HasError() {
		return fmt.Errorf("falha ao ler startup_files")
	}
	sshHost := node
	if client.SSHHost != "" {
		sshHost = client.SSHHost
	}
	for dstPath, srcPath := range mapping {
		content, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("falha ao ler arquivo local %s: %w", srcPath, err)
		}
		mode := os.FileMode(0o644)
		if info, err := os.Stat(srcPath); err == nil && info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		if err := writeFileToContainer(ctx, client, sshHost, vmid, dstPath, content, mode); err != nil {
			return fmt.Errorf("falha ao copiar %s para %s: %w", srcPath, dstPath, err)
		}
	}
	return nil
}

func runPctExec(ctx context.Context, client *Client, host string, vmid int64, command string) error {
	if client.SSHUser == "" || client.SSHKey == "" {
		return fmt.Errorf("credenciais SSH do host ausentes para pct exec")
	}
	args := []string{"pct", "exec", fmt.Sprintf("%d", vmid), "--", "bash", "-lc", command}
	var lastErr error
	for i := 0; i < 36; i++ {
		if err := client.RunSSHCommand(ctx, host, args); err == nil {
			return nil
		} else {
			lastErr = err
			time.Sleep(5 * time.Second)
		}
	}
	return lastErr
}

func writeFileToContainer(ctx context.Context, client *Client, host string, vmid int64, dstPath string, content []byte, mode os.FileMode) error {
	dir := filepathDir(dstPath)
	tmpB64Path := dstPath + ".b64"
	if err := runPctExec(ctx, client, host, vmid, fmt.Sprintf("mkdir -p %s && : > %s", shellEscape(dir), shellEscape(tmpB64Path))); err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(content)
	const chunkSize = 48 * 1024
	for offset := 0; offset < len(encoded); offset += chunkSize {
		end := offset + chunkSize
		if end > len(encoded) {
			end = len(encoded)
		}
		chunk := encoded[offset:end]
		cmd := fmt.Sprintf("printf %%s %s >> %s", shellEscape(chunk), shellEscape(tmpB64Path))
		if err := runPctExec(ctx, client, host, vmid, cmd); err != nil {
			return err
		}
	}
	finalize := fmt.Sprintf("base64 -d %s > %s && chmod %s %s && rm -f %s",
		shellEscape(tmpB64Path),
		shellEscape(dstPath),
		shellEscape(strconv.FormatUint(uint64(mode.Perm()), 8)),
		shellEscape(dstPath),
		shellEscape(tmpB64Path),
	)
	return runPctExec(ctx, client, host, vmid, finalize)
}

func filepathDir(path string) string {
	lastSlash := strings.LastIndex(path, "/")
	if lastSlash <= 0 {
		return "/"
	}
	return path[:lastSlash]
}

func waitForContainerReady(ctx context.Context, client *Client, node string, vmid int64) error {
	if err := client.waitForContainerPresence(ctx, node, vmid, 5*time.Minute); err != nil {
		return fmt.Errorf("container %d ainda nao disponivel no no %s apos aguardar criacao: %w", vmid, node, err)
	}
	return nil
}

func waitForContainerConfigOnHost(ctx context.Context, client *Client, node string, vmid int64) error {
	var lastErr error
	for i := 0; i < 20; i++ {
		if err := client.confirmContainerConfigPresenceOnHost(ctx, node, vmid); err == nil {
			return nil
		} else {
			lastErr = err
			time.Sleep(2 * time.Second)
		}
	}
	return lastErr
}

func shellEscape(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
