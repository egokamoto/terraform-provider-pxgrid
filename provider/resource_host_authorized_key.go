package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &HostAuthorizedKeyResource{}
var _ resource.ResourceWithConfigure = &HostAuthorizedKeyResource{}
var _ resource.ResourceWithImportState = &HostAuthorizedKeyResource{}

type HostAuthorizedKeyResource struct {
	client *Client
}

type hostAuthorizedKeyModel struct {
	ID        types.String `tfsdk:"id"`
	NodeName  types.String `tfsdk:"node_name"`
	Username  types.String `tfsdk:"username"`
	PublicKey types.String `tfsdk:"public_key"`
}

func NewHostAuthorizedKeyResource() resource.Resource {
	return &HostAuthorizedKeyResource{}
}

func (r *HostAuthorizedKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_host_authorized_key"
}

func (r *HostAuthorizedKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Adiciona uma chave pública ao authorized_keys de um usuário no host Proxmox.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Identificador interno node/user.",
			},
			"node_name": schema.StringAttribute{
				Required:    true,
				Description: "Nó alvo/host acessado via SSH.",
			},
			"username": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("root"),
				Description: "Usuário que receberá a chave (default root).",
			},
			"public_key": schema.StringAttribute{
				Required:    true,
				Sensitive:   true,
				Description: "Conteúdo da chave pública (linha completa ssh-rsa/ed25519...).",
			},
		},
	}
}

func (r *HostAuthorizedKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *HostAuthorizedKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan hostAuthorizedKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client := r.selectClient()
	if client == nil {
		resp.Diagnostics.AddError("Provider não configurado", "Client não encontrado.")
		return
	}
	if client.SSHUser == "" && client.SSHPass == "" {
		resp.Diagnostics.AddError("SSH ausente", "Configure ssh_username + ssh_private_key(_file) ou ssh_password no provider para usar pxgrid_host_authorized_key.")
		return
	}
	host := plan.NodeName.ValueString()
	if client.SSHHost != "" {
		host = client.SSHHost
	}
	username := plan.Username.ValueString()
	pub := plan.PublicKey.ValueString()
	cmd := fmt.Sprintf(`
set -euo pipefail
user_home=$(getent passwd %s | cut -d: -f6)
if [ -z "$user_home" ]; then
  echo "Usuário %s inexistente" >&2
  exit 1
fi
ssh_dir="$user_home/.ssh"
auth_file="$ssh_dir/authorized_keys"
mkdir -p "$ssh_dir"
chmod 700 "$ssh_dir"
touch "$auth_file"
chmod 600 "$auth_file"
grep -qxF %s "$auth_file" || echo %s >> "$auth_file"
`, shellEscape(username), username, shellEscape(pub), shellEscape(pub))
	if err := r.execCommand(ctx, client, host, plan.Username.ValueString(), cmd); err != nil {
		resp.Diagnostics.AddError("Falha ao adicionar chave", err.Error())
		return
	}
	plan.ID = types.StringValue(fmt.Sprintf("%s:%s", plan.NodeName.ValueString(), username))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *HostAuthorizedKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state hostAuthorizedKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.ID = types.StringValue(fmt.Sprintf("%s:%s", state.NodeName.ValueString(), state.Username.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *HostAuthorizedKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Atualização não suportada", "Use replace para atualizar a chave autorizada.")
}

func (r *HostAuthorizedKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state hostAuthorizedKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client := r.selectClient()
	if client == nil || (client.SSHUser == "" && client.SSHPass == "") {
		return
	}
	host := state.NodeName.ValueString()
	if client.SSHHost != "" {
		host = client.SSHHost
	}
	username := state.Username.ValueString()
	pub := state.PublicKey.ValueString()
	cmd := fmt.Sprintf(`
set -euo pipefail
user_home=$(getent passwd %s | cut -d: -f6) || exit 0
auth_file="$user_home/.ssh/authorized_keys"
[ -f "$auth_file" ] || exit 0
tmp=$(mktemp)
grep -vxF %s "$auth_file" > "$tmp" || true
cat "$tmp" > "$auth_file"
rm -f "$tmp"
`, shellEscape(username), shellEscape(pub))
	_ = r.execCommand(ctx, client, host, username, cmd)
}

func (r *HostAuthorizedKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func (r *HostAuthorizedKeyResource) selectClient() *Client {
	if r.client != nil {
		return r.client
	}
	return defaultClient
}

func (r *HostAuthorizedKeyResource) execCommand(ctx context.Context, client *Client, host, username, command string) error {
	if client.SSHPass != "" {
		return client.RunSSHCommandPassword(ctx, host, username, client.SSHPass, command)
	}
	return client.RunSSHCommand(ctx, host, []string{"bash", "-lc", command})
}
