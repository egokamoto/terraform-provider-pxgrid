package provider

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &HostPasswordResource{}
var _ resource.ResourceWithConfigure = &HostPasswordResource{}
var _ resource.ResourceWithImportState = &HostPasswordResource{}

type HostPasswordResource struct {
	client *Client
}

type hostPasswordModel struct {
	ID                 types.String `tfsdk:"id"`
	NodeName           types.String `tfsdk:"node_name"`
	Username           types.String `tfsdk:"username"`
	Password           types.String `tfsdk:"password"`
	Unlock             types.Bool   `tfsdk:"unlock"`
	EnablePasswordAuth types.Bool   `tfsdk:"enable_password_auth"`
	PermitRootLogin    types.Bool   `tfsdk:"permit_root_login"`
}

func NewHostPasswordResource() resource.Resource {
	return &HostPasswordResource{}
}

func (r *HostPasswordResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_host_password"
}

func (r *HostPasswordResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Configura a senha de um usuário no host Proxmox via SSH.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Identificador interno node/user.",
			},
			"node_name": schema.StringAttribute{
				Required:    true,
				Description: "Nome do nó ou host alvo do SSH.",
			},
			"username": schema.StringAttribute{
				Required:    true,
				Description: "Usuário existente no host (ex.: root).",
			},
			"password": schema.StringAttribute{
				Required:    true,
				Sensitive:   true,
				Description: "Senha a ser configurada no host.",
			},
			"unlock": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Desbloqueia o usuário após definir a senha.",
				Default:     booldefault.StaticBool(true),
			},
			"enable_password_auth": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Força PasswordAuthentication yes no sshd_config.",
				Default:     booldefault.StaticBool(true),
			},
			"permit_root_login": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Força PermitRootLogin yes (use apenas se necessário).",
				Default:     booldefault.StaticBool(false),
			},
		},
	}
}

func (r *HostPasswordResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *HostPasswordResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan hostPasswordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client := r.resolveClient()
	if client == nil {
		resp.Diagnostics.AddError("Provider não configurado", "Client não encontrado.")
		return
	}
	if client.SSHUser == "" && client.SSHPass == "" {
		resp.Diagnostics.AddError("SSH ausente", "Defina ssh_username + ssh_private_key(_file) ou ssh_password para pxgrid_host_password.")
		return
	}
	if client.SSHKey == "" && client.SSHPass == "" {
		resp.Diagnostics.AddError("SSH ausente", "Informe ssh_private_key(_file) ou ssh_password para pxgrid_host_password.")
		return
	}
	host := plan.NodeName.ValueString()
	if client.SSHHost != "" {
		host = client.SSHHost
	}
	username := plan.Username.ValueString()
	if err := r.runHostCommand(ctx, client, host, fmt.Sprintf("id -u %s >/dev/null 2>&1", shellEscape(username))); err != nil {
		resp.Diagnostics.AddError("Usuário inexistente", fmt.Sprintf("Não foi possível encontrar o usuário %s: %v", username, err))
		return
	}
	payload := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s\n", username, plan.Password.ValueString())))
	setPassword := fmt.Sprintf("echo %s | base64 -d | chpasswd", shellEscape(payload))
	if err := r.runHostCommand(ctx, client, host, setPassword); err != nil {
		resp.Diagnostics.AddError("Erro ao definir senha", err.Error())
		return
	}
	if plan.Unlock.ValueBool() {
		unlock := fmt.Sprintf("passwd -u %s >/dev/null 2>&1 || usermod -U %s >/dev/null 2>&1 || true", shellEscape(username), shellEscape(username))
		_ = r.runHostCommand(ctx, client, host, unlock)
	}
	var sshConfig string
	if plan.EnablePasswordAuth.ValueBool() {
		sshConfig += "if grep -Eq '^[# ]*PasswordAuthentication' /etc/ssh/sshd_config; then sed -i -E 's/^[# ]*PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config; else printf '\\nPasswordAuthentication yes\\n' >> /etc/ssh/sshd_config; fi;"
	}
	if plan.PermitRootLogin.ValueBool() {
		sshConfig += "if grep -Eq '^[# ]*PermitRootLogin' /etc/ssh/sshd_config; then sed -i -E 's/^[# ]*PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config; else printf '\\nPermitRootLogin yes\\n' >> /etc/ssh/sshd_config; fi;"
	}
	if sshConfig != "" {
		cmd := fmt.Sprintf(`set -euo pipefail
if [ -f /etc/ssh/sshd_config ]; then
  %s
  systemctl restart ssh >/dev/null 2>&1 || systemctl restart sshd >/dev/null 2>&1 || service ssh restart >/dev/null 2>&1 || true
fi`, sshConfig)
		_ = r.runHostCommand(ctx, client, host, cmd)
	}
	plan.ID = types.StringValue(fmt.Sprintf("%s:%s", plan.NodeName.ValueString(), plan.Username.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *HostPasswordResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state hostPasswordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.ID = types.StringValue(fmt.Sprintf("%s:%s", state.NodeName.ValueString(), state.Username.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *HostPasswordResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Força replace simples.
	var plan hostPasswordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddError("Atualização não suportada", "Use replace para alterar senha do host.")
}

func (r *HostPasswordResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state hostPasswordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Sem ação destrutiva: remover estado já é suficiente.
}

func (r *HostPasswordResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func (r *HostPasswordResource) resolveClient() *Client {
	if r.client != nil {
		return r.client
	}
	return defaultClient
}

func (r *HostPasswordResource) runHostCommand(ctx context.Context, client *Client, host, command string) error {
	if client.SSHPass != "" {
		user := client.SSHUser
		if user == "" {
			user = "root"
		}
		return client.RunSSHCommandPassword(ctx, host, user, client.SSHPass, command)
	}
	return client.RunSSHCommand(ctx, host, []string{"bash", "-lc", command})
}
