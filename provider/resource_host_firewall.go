package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &HostFirewallResource{}
var _ resource.ResourceWithConfigure = &HostFirewallResource{}
var _ resource.ResourceWithImportState = &HostFirewallResource{}

type HostFirewallResource struct {
	client *Client
}

type hostFirewallModel struct {
	ID               types.String `tfsdk:"id"`
	NodeName         types.String `tfsdk:"node_name"`
	PolicyIn         types.String `tfsdk:"policy_in"`
	PolicyOut        types.String `tfsdk:"policy_out"`
	EnableCluster    types.Bool   `tfsdk:"enable_cluster"`
	ClusterPolicyIn  types.String `tfsdk:"cluster_policy_in"`
	ClusterPolicyOut types.String `tfsdk:"cluster_policy_out"`
}

func NewHostFirewallResource() resource.Resource {
	return &HostFirewallResource{}
}

func (r *HostFirewallResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_host_firewall"
}

func (r *HostFirewallResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Habilita e configura o firewall do Proxmox (pve-firewall) no nó especificado.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Identificador interno node.",
			},
			"node_name": schema.StringAttribute{
				Required:    true,
				Description: "Nó alvo.",
			},
			"policy_in": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Política default de entrada (ACCEPT/DROP).",
				Default:     stringdefault.StaticString("ACCEPT"),
			},
			"policy_out": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Política default de saída (ACCEPT/DROP).",
				Default:     stringdefault.StaticString("ACCEPT"),
			},
			"enable_cluster": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Habilita também o firewall em nível de cluster.",
				Default:     booldefault.StaticBool(true),
			},
			"cluster_policy_in": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Política default de entrada para o firewall de cluster.",
				Default:     stringdefault.StaticString("ACCEPT"),
			},
			"cluster_policy_out": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Política default de saída para o firewall de cluster.",
				Default:     stringdefault.StaticString("ACCEPT"),
			},
		},
	}
}

func (r *HostFirewallResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *HostFirewallResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan hostFirewallModel
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
	if client.SSHUser == "" && client.SSHPass == "" {
		resp.Diagnostics.AddError("SSH ausente", "Defina ssh_username e ssh_private_key(_file) ou ssh_password para pxgrid_host_firewall.")
		return
	}
	host := plan.NodeName.ValueString()
	if client.SSHHost != "" {
		host = client.SSHHost
	}
	nodeCmd := fmt.Sprintf("pvesh set /nodes/%s/firewall/options --enable 1", plan.NodeName.ValueString())
	if err := r.execHost(ctx, client, host, nodeCmd); err != nil {
		resp.Diagnostics.AddError("Falha ao habilitar firewall no nó", err.Error())
		return
	}
	if plan.EnableCluster.ValueBool() {
		clusterCmd := "pvesh set /cluster/firewall/options --enable 1"
		if err := r.execHost(ctx, client, host, clusterCmd); err != nil {
			resp.Diagnostics.AddError("Falha ao habilitar firewall no cluster", err.Error())
			return
		}
	}
	plan.ID = types.StringValue(plan.NodeName.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *HostFirewallResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state hostFirewallModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.ID.IsNull() || state.ID.ValueString() == "" {
		state.ID = types.StringValue(state.NodeName.ValueString())
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *HostFirewallResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan hostFirewallModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddError("Atualização não suportada", "Use replace para alterar o firewall.")
}

func (r *HostFirewallResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state hostFirewallModel
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
	host := state.NodeName.ValueString()
	if client.SSHHost != "" {
		host = client.SSHHost
	}
	disableNode := fmt.Sprintf("pvesh set /nodes/%s/firewall/options --enable 0", state.NodeName.ValueString())
	_ = r.execHost(ctx, client, host, disableNode)
	if state.EnableCluster.ValueBool() {
		_ = r.execHost(ctx, client, host, "pvesh set /cluster/firewall/options --enable 0")
	}
}

func (r *HostFirewallResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func (r *HostFirewallResource) execHost(ctx context.Context, client *Client, host, command string) error {
	if client.SSHPass != "" {
		user := client.SSHUser
		if user == "" {
			user = "root"
		}
		return client.RunSSHCommandPassword(ctx, host, user, client.SSHPass, command)
	}
	return client.RunSSHCommand(ctx, host, []string{"bash", "-lc", command})
}
