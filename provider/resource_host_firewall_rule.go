package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &HostFirewallRuleResource{}
	_ resource.ResourceWithConfigure   = &HostFirewallRuleResource{}
	_ resource.ResourceWithImportState = &HostFirewallRuleResource{}
)

type HostFirewallRuleResource struct {
	client *Client
}

type hostFirewallRuleModel struct {
	ID        types.String `tfsdk:"id"`
	Scope     types.String `tfsdk:"scope"`
	NodeName  types.String `tfsdk:"node_name"`
	Type      types.String `tfsdk:"type"`
	Action    types.String `tfsdk:"action"`
	Source    types.String `tfsdk:"source"`
	Dest      types.String `tfsdk:"dest"`
	Interface types.String `tfsdk:"iface"`
	Comment   types.String `tfsdk:"comment"`
	Enable    types.Bool   `tfsdk:"enable"`
	Pos       types.Int64  `tfsdk:"pos"`
}

func NewHostFirewallRuleResource() resource.Resource {
	return &HostFirewallRuleResource{}
}

func (r *HostFirewallRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_host_firewall_rule"
}

func (r *HostFirewallRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Gerencia uma regra de firewall no Proxmox (node ou cluster).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Identificador interno (scope + pos).",
			},
			"scope": schema.StringAttribute{
				Required:    true,
				Description: "Escopo da regra: node ou cluster.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"node_name": schema.StringAttribute{
				Optional:    true,
				Description: "Nó alvo (obrigatório para scope=node).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"type": schema.StringAttribute{
				Required:    true,
				Description: "Tipo da regra (in/out).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"action": schema.StringAttribute{
				Required:    true,
				Description: "Ação (ACCEPT/DROP/REJECT).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"source": schema.StringAttribute{
				Optional:    true,
				Description: "CIDR de origem (source).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"dest": schema.StringAttribute{
				Optional:    true,
				Description: "CIDR de destino (dest).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"iface": schema.StringAttribute{
				Optional:    true,
				Description: "Interface de rede (ex.: vnetpg).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"comment": schema.StringAttribute{
				Optional:    true,
				Description: "Comentário da regra.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"enable": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Habilita a regra (default true).",
				Default:     booldefault.StaticBool(true),
			},
			"pos": schema.Int64Attribute{
				Computed:    true,
				Description: "Posição da regra no firewall.",
			},
		},
	}
}

func (r *HostFirewallRuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		if defaultClient != nil {
			r.client = defaultClient
		}
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError("Provider mal configurado", "Tipo inesperado em ProviderData.")
		return
	}
	r.client = client
}

func (r *HostFirewallRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan hostFirewallRuleModel
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
	scope := strings.ToLower(plan.Scope.ValueString())
	if scope != "node" && scope != "cluster" {
		resp.Diagnostics.AddError("Scope inválido", "Use node ou cluster.")
		return
	}
	node := plan.NodeName.ValueString()
	if scope == "node" && node == "" {
		resp.Diagnostics.AddError("node_name obrigatório", "Informe node_name quando scope=node.")
		return
	}
	rule := FirewallRule{
		Type:      plan.Type.ValueString(),
		Action:    plan.Action.ValueString(),
		Source:    plan.Source.ValueString(),
		Dest:      plan.Dest.ValueString(),
		Interface: plan.Interface.ValueString(),
		Comment:   plan.Comment.ValueString(),
		Enable:    boolToIntFlag(plan.Enable.ValueBool()),
	}
	var err error
	if scope == "cluster" {
		err = client.CreateClusterFirewallRule(ctx, rule)
	} else {
		err = client.CreateNodeFirewallRule(ctx, node, rule)
	}
	if err != nil {
		resp.Diagnostics.AddError("Falha ao criar regra", err.Error())
		return
	}
	pos, found, listErr := findFirewallRulePos(ctx, client, scope, node, rule)
	if listErr != nil {
		resp.Diagnostics.AddError("Falha ao localizar regra criada", listErr.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Falha ao localizar regra criada", "Regra não encontrada após criação.")
		return
	}
	plan.Pos = types.Int64Value(int64(pos))
	plan.ID = types.StringValue(buildFirewallRuleID(scope, node, pos))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *HostFirewallRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state hostFirewallRuleModel
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
	scope := strings.ToLower(state.Scope.ValueString())
	node := state.NodeName.ValueString()
	pos := int(state.Pos.ValueInt64())
	rules, err := listFirewallRules(ctx, client, scope, node)
	if err != nil {
		resp.Diagnostics.AddError("Falha ao ler regras", err.Error())
		return
	}
	var matched *FirewallRule
	for i := range rules {
		if rules[i].Pos == pos {
			matched = &rules[i]
			break
		}
	}
	if matched == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.Type = types.StringValue(matched.Type)
	state.Action = types.StringValue(matched.Action)
	if matched.Source != "" {
		state.Source = types.StringValue(matched.Source)
	} else {
		state.Source = types.StringNull()
	}
	if matched.Dest != "" {
		state.Dest = types.StringValue(matched.Dest)
	} else {
		state.Dest = types.StringNull()
	}
	if matched.Interface != "" {
		state.Interface = types.StringValue(matched.Interface)
	} else {
		state.Interface = types.StringNull()
	}
	if matched.Comment != "" {
		state.Comment = types.StringValue(matched.Comment)
	} else {
		state.Comment = types.StringNull()
	}
	state.Enable = types.BoolValue(matched.Enable == 1)
	state.ID = types.StringValue(buildFirewallRuleID(scope, node, pos))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *HostFirewallRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Atualização não suportada", "Use replace para alterar a regra.")
}

func (r *HostFirewallRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state hostFirewallRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client := r.client
	if client == nil && defaultClient != nil {
		client = defaultClient
	}
	if client == nil {
		return
	}
	scope := strings.ToLower(state.Scope.ValueString())
	node := state.NodeName.ValueString()
	pos := int(state.Pos.ValueInt64())
	var err error
	if scope == "cluster" {
		err = client.DeleteClusterFirewallRule(ctx, pos)
	} else {
		err = client.DeleteNodeFirewallRule(ctx, node, pos)
	}
	if err != nil {
		resp.Diagnostics.AddError("Falha ao remover regra", err.Error())
	}
}

func (r *HostFirewallRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func listFirewallRules(ctx context.Context, client *Client, scope, node string) ([]FirewallRule, error) {
	if scope == "cluster" {
		return client.ListClusterFirewallRules(ctx)
	}
	return client.ListNodeFirewallRules(ctx, node)
}

func findFirewallRulePos(ctx context.Context, client *Client, scope, node string, rule FirewallRule) (int, bool, error) {
	rules, err := listFirewallRules(ctx, client, scope, node)
	if err != nil {
		return 0, false, err
	}
	pos := -1
	for _, candidate := range rules {
		if matchFirewallRule(candidate, rule) {
			if candidate.Pos > pos {
				pos = candidate.Pos
			}
		}
	}
	if pos == -1 {
		return 0, false, nil
	}
	return pos, true, nil
}

func matchFirewallRule(a, b FirewallRule) bool {
	return strings.EqualFold(a.Type, b.Type) &&
		strings.EqualFold(a.Action, b.Action) &&
		a.Source == b.Source &&
		a.Dest == b.Dest &&
		a.Interface == b.Interface &&
		a.Comment == b.Comment
}

func buildFirewallRuleID(scope, node string, pos int) string {
	if scope == "cluster" {
		return fmt.Sprintf("cluster:%d", pos)
	}
	return fmt.Sprintf("node:%s:%d", node, pos)
}

func boolToIntFlag(v bool) int {
	if v {
		return 1
	}
	return 0
}
