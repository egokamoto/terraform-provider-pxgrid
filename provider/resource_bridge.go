package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &BridgeResource{}
	_ resource.ResourceWithConfigure   = &BridgeResource{}
	_ resource.ResourceWithImportState = &BridgeResource{}
)

type BridgeResource struct {
	client *Client
}

type bridgeModel struct {
	ID          types.String `tfsdk:"id"`
	NodeName    types.String `tfsdk:"node_name"`
	Name        types.String `tfsdk:"name"`
	BridgePort  types.String `tfsdk:"bridge_port"`
	BridgePorts types.List   `tfsdk:"bridge_ports"`
	Autostart   types.Bool   `tfsdk:"autostart"`
	ApplySSH    types.Bool   `tfsdk:"apply_network"`
	IPv4Addr    types.String `tfsdk:"ipv4_address"`
	IPv4CIDR    types.Int64  `tfsdk:"ipv4_cidr"`
	IPv4GW      types.String `tfsdk:"ipv4_gateway"`
	BridgeVIDs  types.List   `tfsdk:"bridge_vids"`
	VLANAware   types.Bool   `tfsdk:"vlan_aware"`
}

func NewBridgeResource() resource.Resource {
	return &BridgeResource{}
}

func (r *BridgeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_network_bridge"
}

func (r *BridgeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Cria bridge Linux no nó Proxmox via API de rede.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Identificador interno (node/name).",
			},
			"node_name": schema.StringAttribute{
				Required:    true,
				Description: "Nó alvo.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Nome da bridge (ex.: vmbr1).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"bridge_port": schema.StringAttribute{
				Optional:    true,
				Description: "Porta física opcional para adicionar à bridge.",
			},
			"bridge_ports": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Lista de portas físicas para compor a bridge (deprecando bridge_port).",
			},
			"ipv4_address": schema.StringAttribute{
				Optional:    true,
				Description: "Endereço IPv4 atribuído à bridge (ex.: 10.11.0.1).",
			},
			"ipv4_cidr": schema.Int64Attribute{
				Optional:    true,
				Description: "Prefixo IPv4 (ex.: 24). Obrigatório se ipv4_address for definido.",
			},
			"ipv4_gateway": schema.StringAttribute{
				Optional:    true,
				Description: "Gateway IPv4 (opcional).",
			},
			"autostart": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Autostart (default true).",
			},
			"vlan_aware": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Habilita VLAN awareness na bridge (default false).",
			},
			"apply_network": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Aplica a configuracao de rede via API do Proxmox apos criar/deletar a bridge.",
			},
			"bridge_vids": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Lista de VLAN IDs/intervalos aceitos pela bridge (ex.: [\"100\", \"200-220\"]).",
			},
		},
	}
}

func (r *BridgeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan bridgeModel
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
	var ports []string
	if !plan.BridgePorts.IsNull() && !plan.BridgePorts.IsUnknown() {
		plan.BridgePorts.ElementsAs(ctx, &ports, false)
	}
	if len(ports) == 0 && !plan.BridgePort.IsNull() && plan.BridgePort.ValueString() != "" {
		ports = append(ports, plan.BridgePort.ValueString())
	}
	ipAddr := ""
	if !plan.IPv4Addr.IsNull() && plan.IPv4Addr.ValueString() != "" {
		ipAddr = plan.IPv4Addr.ValueString()
	}
	ipCidr := int64(0)
	if !plan.IPv4CIDR.IsNull() {
		ipCidr = plan.IPv4CIDR.ValueInt64()
	}
	if ipAddr != "" && ipCidr == 0 {
		resp.Diagnostics.AddError("CIDR obrigatório", "Informe ipv4_cidr quando definir ipv4_address.")
		return
	}
	ipGW := ""
	if !plan.IPv4GW.IsNull() && plan.IPv4GW.ValueString() != "" {
		ipGW = plan.IPv4GW.ValueString()
	}
	exists, err := client.BridgeExists(ctx, plan.NodeName.ValueString(), plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Erro ao validar bridge antes da criação", err.Error())
		return
	}
	if exists {
		resp.Diagnostics.AddError(
			"Bridge já existe",
			fmt.Sprintf("A interface %s já existe no nó %s antes da criação. Importe o recurso para o state ou remova a bridge manualmente antes de reaplicar.", plan.Name.ValueString(), plan.NodeName.ValueString()),
		)
		return
	}
	autostart := true
	if !plan.Autostart.IsNull() && !plan.Autostart.IsUnknown() {
		autostart = plan.Autostart.ValueBool()
	}
	vlanAware := false
	if !plan.VLANAware.IsNull() && !plan.VLANAware.IsUnknown() {
		vlanAware = plan.VLANAware.ValueBool()
	}
	var bridgeVIDs []string
	if !plan.BridgeVIDs.IsNull() && !plan.BridgeVIDs.IsUnknown() {
		plan.BridgeVIDs.ElementsAs(ctx, &bridgeVIDs, false)
	}
	if err := client.CreateBridge(ctx, plan.NodeName.ValueString(), plan.Name.ValueString(), BridgeOptions{
		Ports:       ports,
		Autostart:   autostart,
		IPv4Address: ipAddr,
		IPv4Prefix:  int(ipCidr),
		IPv4Gateway: ipGW,
		VLANAware:   vlanAware,
		BridgeVIDs:  bridgeVIDs,
	}); err != nil {
		resp.Diagnostics.AddError("Erro ao criar bridge", err.Error())
		return
	}
	if plan.ApplySSH.ValueBool() {
		if err := client.ReloadNetwork(ctx, plan.NodeName.ValueString()); err != nil {
			resp.Diagnostics.AddError("Erro ao aplicar rede via API", err.Error())
			return
		}
	}
	if plan.ApplySSH.ValueBool() {
		confirmed := false
		for i := 0; i < 150; i++ {
			ok, err := client.BridgeExists(ctx, plan.NodeName.ValueString(), plan.Name.ValueString())
			if err != nil {
				resp.Diagnostics.AddError("Erro ao confirmar bridge", err.Error())
				return
			}
			if ok {
				confirmed = true
				break
			}
			time.Sleep(2 * time.Second)
		}
		if !confirmed {
			resp.Diagnostics.AddError(
				"Bridge não confirmada após aplicar rede",
				fmt.Sprintf("A interface %s foi criada, mas não ficou visível via API no nó %s após o reload de rede.", plan.Name.ValueString(), plan.NodeName.ValueString()),
			)
			return
		}
	}
	plan.Autostart = types.BoolValue(autostart)
	if plan.ApplySSH.IsNull() || plan.ApplySSH.IsUnknown() {
		plan.ApplySSH = types.BoolValue(false)
	}
	plan.VLANAware = types.BoolValue(vlanAware)
	if ipAddr != "" {
		plan.IPv4Addr = types.StringValue(ipAddr)
		plan.IPv4CIDR = types.Int64Value(ipCidr)
	} else {
		plan.IPv4Addr = types.StringNull()
		plan.IPv4CIDR = types.Int64Null()
	}
	if ipGW != "" {
		plan.IPv4GW = types.StringValue(ipGW)
	} else {
		plan.IPv4GW = types.StringNull()
	}
	if len(ports) > 0 {
		values := make([]attr.Value, len(ports))
		for i, p := range ports {
			values[i] = types.StringValue(p)
		}
		plan.BridgePorts = types.ListValueMust(types.StringType, values)
	} else {
		plan.BridgePorts = types.ListNull(types.StringType)
	}
	if len(bridgeVIDs) > 0 {
		values := make([]attr.Value, len(bridgeVIDs))
		for i, v := range bridgeVIDs {
			values[i] = types.StringValue(v)
		}
		plan.BridgeVIDs = types.ListValueMust(types.StringType, values)
	} else {
		plan.BridgeVIDs = types.ListNull(types.StringType)
	}
	plan.ID = types.StringValue(fmt.Sprintf("%s/%s", plan.NodeName.ValueString(), plan.Name.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *BridgeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state bridgeModel
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
	if ok, _ := client.BridgeExists(ctx, state.NodeName.ValueString(), state.Name.ValueString()); !ok {
		resp.State.RemoveResource(ctx)
		return
	}
	if state.Autostart.IsUnknown() || state.Autostart.IsNull() {
		state.Autostart = types.BoolValue(true)
	}
	if state.ApplySSH.IsUnknown() || state.ApplySSH.IsNull() {
		state.ApplySSH = types.BoolValue(false)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *BridgeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Operação não suportada", "Bridge requer replace.")
}

func (r *BridgeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state bridgeModel
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
	if err := client.DeleteInterface(ctx, state.NodeName.ValueString(), state.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Erro ao deletar bridge", err.Error())
		return
	}
	if state.ApplySSH.ValueBool() {
		if err := client.ReloadNetwork(ctx, state.NodeName.ValueString()); err != nil {
			resp.Diagnostics.AddError("Erro ao aplicar remoção da bridge via API", err.Error())
			return
		}
	}
	if err := waitForBridgeAbsence(ctx, client, state.NodeName.ValueString(), state.Name.ValueString(), state.ApplySSH.ValueBool()); err != nil {
		resp.Diagnostics.AddError("Bridge não removida", err.Error())
		return
	}
}

func waitForBridgeAbsence(ctx context.Context, client *Client, node, bridge string, applied bool) error {
	for i := 0; i < 150; i++ {
		ok, err := client.BridgeExists(ctx, node, bridge)
		if err != nil {
			return fmt.Errorf("erro ao confirmar remoção da bridge %s: %w", bridge, err)
		}
		if !ok {
			return nil
		}
		if applied && i > 0 && i%10 == 0 {
			if err := client.ReloadNetwork(ctx, node); err != nil {
				return fmt.Errorf("erro ao reaplicar rede enquanto aguardava remoção da bridge %s: %w", bridge, err)
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("a interface %s ainda está visível via API no nó %s após o delete", bridge, node)
}

func (r *BridgeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *BridgeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
