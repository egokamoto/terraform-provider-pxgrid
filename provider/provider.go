package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ provider.Provider = &pxGridProvider{}

type pxGridProvider struct{}

// fallback shared client (prototipo)
var defaultClient *Client

type pxGridProviderModel struct {
	Endpoint                 types.String `tfsdk:"endpoint"`
	Insecure                 types.Bool   `tfsdk:"insecure"`
	Username                 types.String `tfsdk:"username"`
	Password                 types.String `tfsdk:"password"`
	APIToken                 types.String `tfsdk:"api_token"`
	SSHUser                  types.String `tfsdk:"ssh_username"`
	SSHKeyFile               types.String `tfsdk:"ssh_private_key_file"`
	SSHKeyInline             types.String `tfsdk:"ssh_private_key"`
	SSHPassword              types.String `tfsdk:"ssh_password"`
	SSHHost                  types.String `tfsdk:"ssh_host"`
	SSHKnownHostsFile        types.String `tfsdk:"ssh_known_hosts_file"`
	SSHStrictHostKeyChecking types.Bool   `tfsdk:"ssh_strict_host_key_checking"`
}

func New() provider.Provider {
	return &pxGridProvider{}
}

func (p *pxGridProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "pxgrid"
}

func (p *pxGridProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	// endpoint + auth (token ou user/pass) + ssh (opcional, para recursos que exijam).
	resp.Schema = schema.Schema{
		Description: "Provider pxgrid para Proxmox VE bootstrap workflows, host networking, SDN lab setup, and LXC initialization.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Required:    true,
				Description: "Endpoint HTTPS do Proxmox (ex.: https://192.168.15.2:8006/).",
			},
			"insecure": schema.BoolAttribute{
				Optional:    true,
				Description: "Ignora validação TLS do endpoint.",
			},
			"username": schema.StringAttribute{
				Optional:    true,
				Description: "Usuário com realm (ex.: root@pam).",
			},
			"password": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Senha para autenticação.",
			},
			"api_token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Token no formato user@realm!name=secret.",
			},
			"ssh_username": schema.StringAttribute{
				Optional:    true,
				Description: "Usuário SSH usado em provisionamento remoto.",
			},
			"ssh_private_key_file": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Caminho para a chave privada SSH (conteúdo lido no apply).",
			},
			"ssh_private_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Conteúdo da chave privada SSH (alternativa ao arquivo).",
			},
			"ssh_password": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Senha SSH usada para comandos remotos (fallback ao uso da chave privada).",
			},
			"ssh_host": schema.StringAttribute{
				Optional:    true,
				Description: "Host/IP usado para SSH (fallback para node_name).",
			},
			"ssh_known_hosts_file": schema.StringAttribute{
				Optional:    true,
				Description: "Caminho para known_hosts (usado quando ssh_strict_host_key_checking=true).",
			},
			"ssh_strict_host_key_checking": schema.BoolAttribute{
				Optional:    true,
				Description: "Habilita verificação estrita da chave SSH do host.",
			},
		},
	}
}

func (p *pxGridProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data pxGridProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.Endpoint.IsNull() || data.Endpoint.IsUnknown() {
		resp.Diagnostics.AddError("Configuração inválida", "endpoint é obrigatório.")
		return
	}
	endpoint := data.Endpoint.ValueString()
	if endpoint == "" {
		resp.Diagnostics.AddError("Configuração inválida", "endpoint é obrigatório.")
		return
	}

	apiToken := data.APIToken.ValueString()
	username := data.Username.ValueString()
	password := data.Password.ValueString()
	sshKey := ""
	if !data.SSHKeyInline.IsNull() && !data.SSHKeyInline.IsUnknown() && data.SSHKeyInline.ValueString() != "" {
		sshKey = data.SSHKeyInline.ValueString()
	} else if !data.SSHKeyFile.IsNull() && !data.SSHKeyFile.IsUnknown() && data.SSHKeyFile.ValueString() != "" {
		path := data.SSHKeyFile.ValueString()
		if after, ok := strings.CutPrefix(path, "~"); ok {
			if home, err := os.UserHomeDir(); err == nil {
				path = filepath.Join(home, after)
			}
		}
		path = filepath.Clean(path)
		b, err := os.ReadFile(path)
		if err != nil {
			resp.Diagnostics.AddError("Falha ao ler ssh_private_key_file", err.Error())
			return
		}
		sshKey = string(b)
	}
	sshHost := data.SSHHost.ValueString()
	sshPassword := ""
	if !data.SSHPassword.IsNull() && !data.SSHPassword.IsUnknown() {
		sshPassword = data.SSHPassword.ValueString()
	}
	sshKnownHosts := ""
	if !data.SSHKnownHostsFile.IsNull() && !data.SSHKnownHostsFile.IsUnknown() {
		sshKnownHosts = data.SSHKnownHostsFile.ValueString()
		if sshKnownHosts != "" {
			if after, ok := strings.CutPrefix(sshKnownHosts, "~"); ok {
				if home, err := os.UserHomeDir(); err == nil {
					sshKnownHosts = filepath.Join(home, after)
				}
			}
			sshKnownHosts = filepath.Clean(sshKnownHosts)
		}
	}
	sshStrict := false
	if !data.SSHStrictHostKeyChecking.IsNull() && !data.SSHStrictHostKeyChecking.IsUnknown() {
		sshStrict = data.SSHStrictHostKeyChecking.ValueBool()
	}

	hasUser := username != ""
	hasPass := password != ""

	tflog.Debug(ctx, "pxgrid provider credentials check", map[string]interface{}{
		"has_username": hasUser,
		"has_password": hasPass,
		"has_token":    apiToken != "",
	})

	client := NewClient(endpoint, data.Insecure.ValueBool(), apiToken, username, password)
	if !data.SSHUser.IsNull() && !data.SSHUser.IsUnknown() {
		client.WithSSH(data.SSHUser.ValueString(), sshKey, sshHost, sshPassword)
	} else if sshPassword != "" {
		client.WithSSH("", "", sshHost, sshPassword)
	}
	client.SSHStrictHostKeyChecking = sshStrict
	client.SSHKnownHostsFile = sshKnownHosts
	defaultClient = client
	resp.ResourceData = client
	resp.DataSourceData = client
	tflog.Debug(ctx, "pxgrid provider configured", map[string]interface{}{"endpoint": endpoint})
}

func (p *pxGridProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewUserResource,
		NewUserTokenResource,
		NewSDNZoneVLANResource,
		NewSDNVNetResource,
		NewContainerResource,
		NewTemplateResource,
		NewBridgeResource,
		NewHostPasswordResource,
		NewHostNATResource,
		NewHostAuthorizedKeyResource,
		NewHostFirewallResource,
		NewHostFirewallRuleResource,
	}
}

func (p *pxGridProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewTemplateDataSource,
	}
}
