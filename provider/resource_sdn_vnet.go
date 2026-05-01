package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &SDNVNetResource{}
	_ resource.ResourceWithConfigure   = &SDNVNetResource{}
	_ resource.ResourceWithImportState = &SDNVNetResource{}
)

type SDNVNetResource struct {
	client *Client
}

type sdnVNetModel struct {
	ID           types.String `tfsdk:"id"`
	Zone         types.String `tfsdk:"zone"`
	Alias        types.String `tfsdk:"alias"`
	Tag          types.Int64  `tfsdk:"tag"`
	IsolatePorts types.Bool   `tfsdk:"isolate_ports"`
	VLANAware    types.Bool   `tfsdk:"vlan_aware"`
}

func NewSDNVNetResource() resource.Resource {
	return &SDNVNetResource{}
}

func (r *SDNVNetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sdn_vnet"
}

func (r *SDNVNetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Cria VNET associada a uma zona SDN VLAN.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "Identificador da VNET (ex.: vnet100).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"zone": schema.StringAttribute{
				Required:    true,
				Description: "Zona SDN alvo.",
			},
			"alias": schema.StringAttribute{
				Optional:    true,
				Description: "Alias opcional.",
			},
			"tag": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "Tag VLAN/VXLAN (quando aplicável).",
				Default:     int64default.StaticInt64(0),
			},
			"isolate_ports": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Isola portas dentro da VNET.",
				Default:     booldefault.StaticBool(false),
			},
			"vlan_aware": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Permite VLANs dentro da VNET.",
				Default:     booldefault.StaticBool(false),
			},
		},
	}
}

func (r *SDNVNetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sdnVNetModel
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

	v := SDNVNet{
		ID:           plan.ID.ValueString(),
		Zone:         plan.Zone.ValueString(),
		Alias:        plan.Alias.ValueString(),
		Tag:          plan.Tag.ValueInt64(),
		IsolatePorts: plan.IsolatePorts.ValueBool(),
		VLANAware:    plan.VLANAware.ValueBool(),
	}
	if err := client.CreateSDNVNet(ctx, v); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == 500 {
			if existing, getErr := client.GetSDNVNet(ctx, v.ID); getErr == nil {
				plan.Zone = types.StringValue(existing.Zone)
				plan.Alias = types.StringValue(existing.Alias)
				plan.Tag = types.Int64Value(existing.Tag)
				plan.IsolatePorts = types.BoolValue(existing.IsolatePorts)
				plan.VLANAware = types.BoolValue(existing.VLANAware)
			} else {
				resp.Diagnostics.AddError("Erro ao criar VNET", fmt.Sprintf("VNET já existe e leitura falhou: %s", getErr.Error()))
				return
			}
		} else {
			resp.Diagnostics.AddError("Erro ao criar VNET", err.Error())
			return
		}
	}
	_ = client.ApplySDNVNet(ctx, v.ID)
	waitForBridge(ctx, client, v.ID, 150)
	if plan.Alias.ValueString() == "" {
		plan.Alias = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SDNVNetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sdnVNetModel
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
	v, err := client.GetSDNVNet(ctx, state.ID.ValueString())
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Erro ao ler VNET", err.Error())
		return
	}
	state.Zone = types.StringValue(v.Zone)
	state.Alias = types.StringNull()
	if v.Alias != "" {
		state.Alias = types.StringValue(v.Alias)
	}
	state.Tag = types.Int64Value(v.Tag)
	state.IsolatePorts = types.BoolValue(v.IsolatePorts)
	state.VLANAware = types.BoolValue(v.VLANAware)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *SDNVNetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan sdnVNetModel
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
	v := SDNVNet{
		ID:           plan.ID.ValueString(),
		Zone:         plan.Zone.ValueString(),
		Alias:        plan.Alias.ValueString(),
		Tag:          plan.Tag.ValueInt64(),
		IsolatePorts: plan.IsolatePorts.ValueBool(),
		VLANAware:    plan.VLANAware.ValueBool(),
	}
	if err := client.UpdateSDNVNet(ctx, v); err != nil {
		resp.Diagnostics.AddError("Erro ao atualizar VNET", err.Error())
		return
	}
	_ = client.ApplySDNVNet(ctx, v.ID)
	waitForBridge(ctx, client, v.ID, 150)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SDNVNetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sdnVNetModel
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
	if err := client.DeleteSDNVNet(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Erro ao deletar VNET", err.Error())
	}
}

func (r *SDNVNetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SDNVNetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func waitForBridge(ctx context.Context, client *Client, bridge string, attempts int) {
	if client == nil {
		return
	}
	for i := 0; i < attempts; i++ {
		ok, err := client.BridgeExists(ctx, "pve", bridge) // usa nó padrão configurado
		if err == nil && ok {
			return
		}
		time.Sleep(2 * time.Second)
	}
}
