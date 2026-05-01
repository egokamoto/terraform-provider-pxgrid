package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &TemplateResource{}
	_ resource.ResourceWithConfigure   = &TemplateResource{}
	_ resource.ResourceWithImportState = &TemplateResource{}
)

type TemplateResource struct {
	client *Client
}

type templateModel struct {
	ID        types.String `tfsdk:"id"`
	NodeName  types.String `tfsdk:"node_name"`
	Storage   types.String `tfsdk:"storage"`
	FileName  types.String `tfsdk:"file_name"`
	SourceURL types.String `tfsdk:"source_url"`
	VerifyTLS types.Bool   `tfsdk:"verify_tls"`
}

func NewTemplateResource() resource.Resource {
	return &TemplateResource{}
}

func (r *TemplateResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_template"
}

func (r *TemplateResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Baixa template LXC para um storage do Proxmox.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Volid do template (ex.: local:vztmpl/debian-12...tar.zst).",
			},
			"node_name": schema.StringAttribute{
				Required:    true,
				Description: "Nó alvo onde o download será executado.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"storage": schema.StringAttribute{
				Required:    true,
				Description: "Storage alvo (ex.: local).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"file_name": schema.StringAttribute{
				Required:    true,
				Description: "Nome do template (ex.: debian-12-standard_12.0-1_amd64.tar.zst).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"source_url": schema.StringAttribute{
				Optional:    true,
				Description: "URL completa para download do template (usada se o arquivo não existir).",
			},
			"verify_tls": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Valida certificado do download (default true). Se false e a URL for https, será trocada para http.",
			},
		},
	}
}

func (r *TemplateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan templateModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	verifyTLS := true
	if !plan.VerifyTLS.IsNull() && !plan.VerifyTLS.IsUnknown() {
		verifyTLS = plan.VerifyTLS.ValueBool()
	}
	plan.VerifyTLS = types.BoolValue(verifyTLS)
	client := r.client
	if client == nil && defaultClient != nil {
		client = defaultClient
	}
	if client == nil {
		resp.Diagnostics.AddError("Provider não configurado", "Client não encontrado.")
		return
	}
	fileName := plan.FileName.ValueString()
	storage := plan.Storage.ValueString()
	node := plan.NodeName.ValueString()
	exists, err := client.TemplateExists(ctx, node, storage, fileName)
	if err != nil {
		resp.Diagnostics.AddError("Erro ao verificar template", err.Error())
		return
	}
	if !exists {
		sourceURL := plan.SourceURL.ValueString()
		if plan.SourceURL.IsNull() || sourceURL == "" {
			resp.Diagnostics.AddError("Template ausente", "Informe source_url ou garanta que o template já está presente.")
			return
		}
		effectiveURL := sourceURL
		if !verifyTLS && strings.HasPrefix(sourceURL, "https://") {
			effectiveURL = "http://" + strings.TrimPrefix(sourceURL, "https://")
		}
		if err := client.DownloadTemplate(ctx, node, storage, fileName, effectiveURL); err != nil {
			resp.Diagnostics.AddError("Erro ao baixar template", fmt.Sprintf("%s (url usada: %s)", err.Error(), effectiveURL))
			return
		}
		for i := 0; i < 30; i++ {
			ok, err := client.TemplateExists(ctx, node, storage, fileName)
			if err != nil {
				resp.Diagnostics.AddError("Erro ao verificar template após download", err.Error())
				return
			}
			if ok {
				exists = true
				break
			}
			time.Sleep(2 * time.Second)
		}
		if !exists {
			resp.Diagnostics.AddError("Template não disponível", "Download iniciado, mas o arquivo não apareceu no storage após aguardar.")
			return
		}
	}
	plan.ID = types.StringValue(fmt.Sprintf("%s:vztmpl/%s", storage, fileName))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *TemplateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state templateModel
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
	exists, err := client.TemplateExists(ctx, state.NodeName.ValueString(), state.Storage.ValueString(), state.FileName.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Erro ao ler template", err.Error())
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *TemplateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan templateModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddError("Operação não suportada", "Template requer replace para alterar atributos.")
}

func (r *TemplateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state templateModel
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
	if err := client.DeleteTemplate(ctx, state.NodeName.ValueString(), state.Storage.ValueString(), state.FileName.ValueString()); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == 404 {
			return
		}
		resp.Diagnostics.AddError("Erro ao deletar template", err.Error())
	}
}

func (r *TemplateResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *TemplateResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
