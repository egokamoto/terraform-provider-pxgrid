package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &SDNZoneVLANResource{}
	_ resource.ResourceWithConfigure   = &SDNZoneVLANResource{}
	_ resource.ResourceWithImportState = &SDNZoneVLANResource{}
)

type SDNZoneVLANResource struct {
	client *Client
}

type sdnZoneVLANModel struct {
	ID         types.String `tfsdk:"id"`
	Bridge     types.String `tfsdk:"bridge"`
	MTU        types.Int64  `tfsdk:"mtu"`
	Nodes      types.List   `tfsdk:"nodes"`
	DNS        types.String `tfsdk:"dns"`
	DNSZone    types.String `tfsdk:"dns_zone"`
	IPAM       types.String `tfsdk:"ipam"`
	ReverseDNS types.String `tfsdk:"reverse_dns"`
}

func NewSDNZoneVLANResource() resource.Resource {
	return &SDNZoneVLANResource{}
}

func (r *SDNZoneVLANResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sdn_zone_vlan"
}

func (r *SDNZoneVLANResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Cria zona SDN VLAN no Proxmox.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "Identificador da zona (ex.: zone100).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"bridge": schema.StringAttribute{
				Required:    true,
				Description: "Bridge Linux presente em todos os nós (ex.: vmbr100).",
			},
			"mtu": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "MTU da zona.",
				Default:     int64default.StaticInt64(0),
			},
			"nodes": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Nós onde a zona será aplicada (omita para todos).",
			},
			"dns": schema.StringAttribute{
				Optional:    true,
				Description: "Servidor DNS opcional.",
			},
			"dns_zone": schema.StringAttribute{
				Optional:    true,
				Description: "Zona DNS para registros.",
			},
			"ipam": schema.StringAttribute{
				Optional:    true,
				Description: "Backend de IPAM (ex.: pve).",
			},
			"reverse_dns": schema.StringAttribute{
				Optional:    true,
				Description: "Servidor DNS reverso.",
			},
		},
	}
}

func (r *SDNZoneVLANResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sdnZoneVLANModel
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

	nodes, di := listToStrings(ctx, plan.Nodes)
	resp.Diagnostics.Append(di...)
	if resp.Diagnostics.HasError() {
		return
	}

	zone := SDNZoneVLAN{
		ID:         plan.ID.ValueString(),
		Bridge:     plan.Bridge.ValueString(),
		MTU:        plan.MTU.ValueInt64(),
		Nodes:      nodes,
		DNS:        plan.DNS.ValueString(),
		DNSZone:    plan.DNSZone.ValueString(),
		IPAM:       plan.IPAM.ValueString(),
		ReverseDNS: plan.ReverseDNS.ValueString(),
		Type:       "vlan",
	}

	if err := client.CreateSDNZoneVLAN(ctx, zone); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusInternalServerError && strings.Contains(strings.ToLower(apiErr.Body), "already defined") {
			existing, getErr := client.GetSDNZoneVLAN(ctx, zone.ID)
			if getErr != nil {
				resp.Diagnostics.AddError("Erro ao criar zona SDN VLAN", fmt.Sprintf("zona já existe mas leitura falhou: %s", getErr.Error()))
				return
			}
			plan.Bridge = types.StringValue(existing.Bridge)
			plan.MTU = types.Int64Value(existing.MTU)
			plan.DNS = stringOrNull(existing.DNS)
			plan.DNSZone = stringOrNull(existing.DNSZone)
			plan.IPAM = stringOrNull(existing.IPAM)
			plan.ReverseDNS = stringOrNull(existing.ReverseDNS)
			plan.Nodes = stringSliceOrNull(existing.Nodes)
		} else {
			resp.Diagnostics.AddError("Erro ao criar zona SDN VLAN", err.Error())
			return
		}
	}
	_ = client.ApplySDNZone(ctx, zone.ID) // melhor esforço
	waitForZoneApply(ctx, client, zone.ID, 150)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SDNZoneVLANResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sdnZoneVLANModel
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
	zone, err := client.GetSDNZoneVLAN(ctx, state.ID.ValueString())
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Erro ao ler zona SDN VLAN", err.Error())
		return
	}
	state.Bridge = types.StringValue(zone.Bridge)
	state.MTU = types.Int64Value(zone.MTU)
	state.DNS = stringOrNull(zone.DNS)
	state.DNSZone = stringOrNull(zone.DNSZone)
	state.IPAM = stringOrNull(zone.IPAM)
	state.ReverseDNS = stringOrNull(zone.ReverseDNS)
	state.Nodes = stringSliceOrNull(zone.Nodes)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *SDNZoneVLANResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan sdnZoneVLANModel
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
	nodes, di := listToStrings(ctx, plan.Nodes)
	resp.Diagnostics.Append(di...)
	if resp.Diagnostics.HasError() {
		return
	}
	form := SDNZoneVLAN{
		ID:         plan.ID.ValueString(),
		Bridge:     plan.Bridge.ValueString(),
		MTU:        plan.MTU.ValueInt64(),
		Nodes:      nodes,
		DNS:        plan.DNS.ValueString(),
		DNSZone:    plan.DNSZone.ValueString(),
		IPAM:       plan.IPAM.ValueString(),
		ReverseDNS: plan.ReverseDNS.ValueString(),
		Type:       "vlan",
	}
	if err := client.UpdateSDNZoneVLAN(ctx, form); err != nil {
		resp.Diagnostics.AddError("Erro ao atualizar zona SDN VLAN", err.Error())
		return
	}
	_ = client.ApplySDNZone(ctx, form.ID)
	_ = client.ReloadNetwork(ctx, coalesceNode(plan.Nodes))
	waitForZoneApply(ctx, client, form.ID, 150)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SDNZoneVLANResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sdnZoneVLANModel
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
	if err := client.DeleteSDNZoneVLAN(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Erro ao deletar zona SDN VLAN", err.Error())
	}
}

func (r *SDNZoneVLANResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		if defaultClient != nil {
			r.client = defaultClient
		}
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError("Provider mal configurado", fmt.Sprintf("Tipo inesperado: %T", req.ProviderData))
		return
	}
	r.client = client
}

func (r *SDNZoneVLANResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func waitForZoneApply(ctx context.Context, client *Client, zone string, attempts int) {
	if client == nil {
		return
	}
	for i := 0; i < attempts; i++ {
		zoneCfg, err := client.GetSDNZoneVLAN(ctx, zone)
		if err == nil && zoneCfg != nil && !zoneCfg.Pending {
			time.Sleep(2 * time.Second) // pequena espera para o daemon aplicar
			return
		}
		time.Sleep(2 * time.Second)
	}
}

func coalesceNode(list types.List) string {
	if list.IsNull() || list.IsUnknown() {
		return ""
	}
	var nodes []string
	list.ElementsAs(context.Background(), &nodes, false)
	if len(nodes) > 0 {
		return nodes[0]
	}
	return ""
}

func stringOrNull(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

func stringSliceOrNull(values []string) types.List {
	if len(values) == 0 {
		return types.ListNull(types.StringType)
	}
	list, _ := stringSliceToList(values)
	return list
}
