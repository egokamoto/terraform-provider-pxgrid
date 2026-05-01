package provider

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &UserTokenResource{}
	_ resource.ResourceWithConfigure   = &UserTokenResource{}
	_ resource.ResourceWithImportState = &UserTokenResource{}
)

type UserTokenResource struct {
	client *Client
}

type userTokenModel struct {
	ID                   types.String `tfsdk:"id"`
	UserID               types.String `tfsdk:"user_id"`
	TokenName            types.String `tfsdk:"token_name"`
	Comment              types.String `tfsdk:"comment"`
	ExpirationDate       types.String `tfsdk:"expiration_date"`
	PrivilegesSeparation types.Bool   `tfsdk:"privileges_separation"`
	Value                types.String `tfsdk:"value"`
}

func NewUserTokenResource() resource.Resource {
	return &UserTokenResource{}
}

func (r *UserTokenResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user_token"
}

func (r *UserTokenResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Cria token de API para um usuário Proxmox.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Identificador do token (<user>!<token>).",
			},
			"user_id": schema.StringAttribute{
				Required:    true,
				Description: "Usuário alvo (ex.: codex@pve).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"token_name": schema.StringAttribute{
				Required:    true,
				Description: "Nome do token.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"comment": schema.StringAttribute{
				Optional:    true,
				Description: "Comentário.",
			},
			"expiration_date": schema.StringAttribute{
				Optional:    true,
				Description: "Data de expiração (RFC3339).",
			},
			"privileges_separation": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Habilita privilege separation no token.",
				Default:     booldefault.StaticBool(false),
			},
			"value": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "Valor do token retornado somente na criação.",
			},
		},
	}
}

func (r *UserTokenResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan userTokenModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil && defaultClient != nil {
		r.client = defaultClient
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Provider não configurado", "Client não encontrado. Configure o provider.")
		return
	}

	expireTS, diagExp := parseExpire(plan.ExpirationDate)
	resp.Diagnostics.Append(diagExp...)
	if resp.Diagnostics.HasError() {
		return
	}

	token, err := r.client.CreateToken(
		ctx,
		plan.UserID.ValueString(),
		plan.TokenName.ValueString(),
		plan.Comment.ValueString(),
		expireTS,
		plan.PrivilegesSeparation.ValueBool(),
	)
	if err != nil {
		resp.Diagnostics.AddError("Erro ao criar token", err.Error())
		return
	}
	plan.ID = types.StringValue(token.TokenID)
	if token.Value != "" {
		plan.Value = types.StringValue(token.Value)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *UserTokenResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state userTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil && defaultClient != nil {
		r.client = defaultClient
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Provider não configurado", "Client não encontrado. Configure o provider.")
		return
	}
	tok, err := r.client.GetToken(ctx, state.UserID.ValueString(), state.TokenName.ValueString())
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Erro ao ler token", err.Error())
		return
	}
	state.ID = types.StringValue(tok.TokenID)
	// Valor do token não é retornado após criação; manter estado se existir.
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *UserTokenResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Atualizações não suportadas; forçar replace.
	var plan userTokenModel
	_ = plan
	resp.Diagnostics.AddError("Operação não suportada", "pxgrid_user_token requer recriação para mudanças.")
}

func (r *UserTokenResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state userTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil && defaultClient != nil {
		r.client = defaultClient
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Provider não configurado", "Client não encontrado. Configure o provider.")
		return
	}
	if err := r.client.DeleteToken(ctx, state.UserID.ValueString(), state.TokenName.ValueString()); err != nil {
		resp.Diagnostics.AddError("Erro ao deletar token", err.Error())
	}
}

func parseExpire(v types.String) (int64, diag.Diagnostics) {
	var diags diag.Diagnostics
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return 0, diags
	}
	t, err := time.Parse(time.RFC3339, v.ValueString())
	if err != nil {
		diags.AddError("Data inválida", "expiration_date deve estar em RFC3339")
		return 0, diags
	}
	return t.Unix(), diags
}

func (r *UserTokenResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		if defaultClient != nil {
			r.client = defaultClient
		}
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError("Provider mal configurado", "Tipo inesperado em ProviderData")
		return
	}
	r.client = client
}

func (r *UserTokenResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "!", 2)
	if len(parts) != 2 {
		resp.Diagnostics.AddError("Formato inválido", "Use <user_id>!<token_name> para importar.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("token_name"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
