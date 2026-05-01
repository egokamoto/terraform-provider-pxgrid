package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &TemplateDataSource{}
var _ datasource.DataSourceWithConfigure = &TemplateDataSource{}

type TemplateDataSource struct {
	client *Client
}

type templateDataModel struct {
	ID       types.String `tfsdk:"id"`
	NodeName types.String `tfsdk:"node_name"`
	Storage  types.String `tfsdk:"storage"`
	FileName types.String `tfsdk:"file_name"`
}

func NewTemplateDataSource() datasource.DataSource {
	return &TemplateDataSource{}
}

func (d *TemplateDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_template"
}

func (d *TemplateDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Consulta template LXC disponível no storage.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Volid do template.",
			},
			"node_name": schema.StringAttribute{
				Required:    true,
				Description: "Nó onde o storage reside.",
			},
			"storage": schema.StringAttribute{
				Required:    true,
				Description: "Storage (ex.: local).",
			},
			"file_name": schema.StringAttribute{
				Required:    true,
				Description: "Nome do template (ex.: debian-12-standard_12.0-1_amd64.tar.zst).",
			},
		},
	}
}

func (d *TemplateDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data templateDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client := d.client
	if client == nil && defaultClient != nil {
		client = defaultClient
	}
	if client == nil {
		resp.Diagnostics.AddError("Provider não configurado", "Client não encontrado.")
		return
	}
	exists, err := client.TemplateExists(ctx, data.NodeName.ValueString(), data.Storage.ValueString(), data.FileName.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Erro ao ler template", err.Error())
		return
	}
	if !exists {
		resp.Diagnostics.AddError("Template não encontrado", fmt.Sprintf("Template %s:vztmpl/%s não existe", data.Storage.ValueString(), data.FileName.ValueString()))
		return
	}
	data.ID = types.StringValue(fmt.Sprintf("%s:vztmpl/%s", data.Storage.ValueString(), data.FileName.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *TemplateDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		if defaultClient != nil {
			d.client = defaultClient
		}
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError("Provider mal configurado", fmt.Sprintf("Tipo inesperado em ProviderData"))
		return
	}
	d.client = client
}
