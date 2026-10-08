package provider

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/klinux/terraform-provider-gravitee/internal/client"
)

type applicationDataSource struct {
	c *client.Client
}

func NewApplicationDataSource() datasource.DataSource { return &applicationDataSource{} }

type applicationDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	ClientID    types.String `tfsdk:"client_id"`
	AppType     types.String `tfsdk:"app_type"`
	Groups      types.Set    `tfsdk:"groups"`
	Status      types.String `tfsdk:"status"`
	Type        types.String `tfsdk:"type"`
}

func (d *applicationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application"
}

func (d *applicationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an application by `id` or by `name`, without managing it. Use it to subscribe an application that something else owns — a console operator or a script — without bringing it under Terraform first.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Application UUID. Give this or `name`.",
			},
			"name": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Exact application name. Archived applications are ignored. Fails when more than one active application carries the name, rather than picking one silently.",
			},
			"description": schema.StringAttribute{Computed: true},
			"client_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "`settings.app.client_id`.",
			},
			"app_type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "`settings.app.type`.",
			},
			"groups": schema.SetAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "UUIDs of the groups that own the application.",
			},
			"status": schema.StringAttribute{Computed: true},
			"type":   schema.StringAttribute{Computed: true},
		},
	}
}

func (d *applicationDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("unexpected ProviderData", fmt.Sprintf("expected *client.Client, got %T", req.ProviderData))
		return
	}
	d.c = c
}

func (d *applicationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m applicationDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	temID := !m.ID.IsNull() && m.ID.ValueString() != ""
	temNome := !m.Name.IsNull() && m.Name.ValueString() != ""
	if temID == temNome {
		resp.Diagnostics.AddError("set `id` or `name`",
			"This data source needs exactly one of the two: `id` to look up by UUID, or `name` to look up by exact name.")
		return
	}

	var app *client.Application
	var err error
	if temID {
		app, err = d.c.GetApplication(ctx, m.ID.ValueString())
	} else {
		app, err = d.c.AchaApplicationPorNome(ctx, m.Name.ValueString())
	}
	if err != nil {
		resp.Diagnostics.AddError("looking up the application", err.Error())
		return
	}

	m.ID = types.StringValue(app.ID)
	m.Name = types.StringValue(app.Name)
	m.Description = types.StringValue(app.Description)
	m.Status = types.StringValue(app.Status)
	m.Type = types.StringValue(app.Type)
	if app.Settings != nil && app.Settings.App != nil {
		m.ClientID = types.StringValue(app.Settings.App.ClientID)
		m.AppType = types.StringValue(app.Settings.App.Type)
	} else {
		m.ClientID = types.StringNull()
		m.AppType = types.StringNull()
	}
	grupos := append([]string(nil), client.GruposDe(app)...)
	sort.Strings(grupos)
	if len(grupos) == 0 {
		m.Groups = types.SetNull(types.StringType)
	} else {
		g, dg := types.SetValueFrom(ctx, types.StringType, grupos)
		resp.Diagnostics.Append(dg...)
		m.Groups = g
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
