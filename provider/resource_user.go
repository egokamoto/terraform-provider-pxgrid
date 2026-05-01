package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &UserResource{}
	_ resource.ResourceWithConfigure   = &UserResource{}
	_ resource.ResourceWithImportState = &UserResource{}
)

type UserResource struct {
	client *Client
}

type userResourceModel struct {
	ID        types.String `tfsdk:"id"`
	UserID    types.String `tfsdk:"user_id"`
	Password  types.String `tfsdk:"password"`
	Comment   types.String `tfsdk:"comment"`
	Enabled   types.Bool   `tfsdk:"enabled"`
	Groups    types.List   `tfsdk:"groups"`
	Email     types.String `tfsdk:"email"`
	ACLPath   types.String `tfsdk:"acl_path"`
	ACLRole   types.String `tfsdk:"acl_role"`
	Propagate types.Bool   `tfsdk:"propagate"`
}

func NewUserResource() resource.Resource {
	return &UserResource{}
}

func (r *UserResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *UserResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Cria usuário no Proxmox com ACL básica.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "user_id gerenciado (igual ao user_id).",
			},
			"user_id": schema.StringAttribute{
				Required:    true,
				Description: "Identificador do usuário (ex.: codex@pve).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"password": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Senha do usuário.",
			},
			"comment": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Comentário.",
				Default:     stringdefault.StaticString("Managed by pxgrid"),
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Habilita o usuário (default true).",
				Default:     booldefault.StaticBool(true),
			},
			"groups": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Grupos do usuário.",
			},
			"email": schema.StringAttribute{
				Optional:    true,
				Description: "Email opcional.",
			},
			"acl_path": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Caminho da ACL (default /).",
				Default:     stringdefault.StaticString("/"),
			},
			"acl_role": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Role aplicada (ex.: Administrator).",
				Default:     stringdefault.StaticString("Administrator"),
			},
			"propagate": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Propaga ACL para subcaminhos (default true).",
				Default:     booldefault.StaticBool(true),
			},
		},
	}
}

func (r *UserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan userResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client := r.client
	if client == nil && defaultClient != nil {
		client = defaultClient
	}
	if client == nil {
		resp.Diagnostics.AddError("Provider não configurado", "Client não encontrado. Configure o provider.")
		return
	}

	groups, dg := listToStrings(ctx, plan.Groups)
	resp.Diagnostics.Append(dg...)
	if resp.Diagnostics.HasError() {
		return
	}

	user := User{
		UserID:  plan.UserID.ValueString(),
		Comment: plan.Comment.ValueString(),
		Enable:  boolToInt(plan.Enabled.ValueBool()),
		Email:   plan.Email.ValueString(),
		Groups:  groups,
	}

	if err := client.CreateUser(ctx, user, plan.Password.ValueString()); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == 500 && strings.Contains(strings.ToLower(apiErr.Body), "already exists") {
			// usuário já existe, segue para aplicar ACL/estado
		} else {
			resp.Diagnostics.AddError("Erro ao criar usuário", err.Error())
			return
		}
	}
	if err := client.AddACL(ctx, plan.ACLPath.ValueString(), user.UserID, plan.ACLRole.ValueString(), plan.Propagate.ValueBool()); err != nil {
		resp.Diagnostics.AddError("Erro ao aplicar ACL", err.Error())
		return
	}

	plan.ID = plan.UserID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *UserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state userResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client := r.client
	if client == nil {
		resp.Diagnostics.AddError("Provider não configurado", "Client não encontrado. Configure o provider.")
		return
	}
	user, err := client.GetUser(ctx, state.UserID.ValueString())
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Erro ao ler usuário", err.Error())
		return
	}
	// Atualiza estado com dados atuais
	state.ID = types.StringValue(user.UserID)
	state.UserID = types.StringValue(user.UserID)
	state.Comment = types.StringValue(user.Comment)
	state.Enabled = types.BoolValue(user.Enable != 0)
	state.Email = types.StringValue(user.Email)
	state.Groups, _ = stringSliceToList(user.Groups)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *UserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan userResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client := r.client
	if client == nil && defaultClient != nil {
		client = defaultClient
	}
	if client == nil {
		resp.Diagnostics.AddError("Provider não configurado", "Client não encontrado. Configure o provider.")
		return
	}
	groups, dg := listToStrings(ctx, plan.Groups)
	resp.Diagnostics.Append(dg...)
	if resp.Diagnostics.HasError() {
		return
	}
	user := User{
		UserID:  plan.UserID.ValueString(),
		Comment: plan.Comment.ValueString(),
		Enable:  boolToInt(plan.Enabled.ValueBool()),
		Email:   plan.Email.ValueString(),
		Groups:  groups,
	}
	if err := client.UpdateUser(ctx, user); err != nil {
		resp.Diagnostics.AddError("Erro ao atualizar usuário", err.Error())
		return
	}
	if err := client.AddACL(ctx, plan.ACLPath.ValueString(), user.UserID, plan.ACLRole.ValueString(), plan.Propagate.ValueBool()); err != nil {
		resp.Diagnostics.AddError("Erro ao aplicar ACL", err.Error())
		return
	}
	plan.ID = plan.UserID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *UserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state userResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client := r.client
	if client == nil && defaultClient != nil {
		client = defaultClient
	}
	if client == nil {
		resp.Diagnostics.AddError("Provider não configurado", "Client não encontrado. Configure o provider.")
		return
	}
	if err := client.DeleteUser(ctx, state.UserID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Erro ao deletar usuário", err.Error())
	}
}

func (r *UserResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *UserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func listToStrings(ctx context.Context, l types.List) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics
	if l.IsNull() || l.IsUnknown() {
		return []string{}, diags
	}
	var res []string
	diags.Append(l.ElementsAs(ctx, &res, false)...)
	return res, diags
}

func stringSliceToList(values []string) (types.List, diag.Diagnostics) {
	if values == nil {
		return types.ListNull(types.StringType), nil
	}
	tvals := make([]attr.Value, 0, len(values))
	for _, v := range values {
		tvals = append(tvals, types.StringValue(v))
	}
	return types.ListValue(types.StringType, tvals)
}
