package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/klinux/terraform-provider-gravitee/internal/client"
)

type apiDataSource struct {
	c *client.Client
}

func NewAPIDataSource() datasource.DataSource { return &apiDataSource{} }

type apiDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	ContextPath types.String `tfsdk:"context_path"`
	State       types.String `tfsdk:"state"`
	PlanIDs     types.Map    `tfsdk:"plan_ids"`
}

func (d *apiDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api"
}

func (d *apiDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an API by `id` or by `name`, without managing it. Use it to subscribe to the plans of an API this configuration does not own: `plan_ids` removes the need to hardcode plan UUIDs, which differ between environments.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "API UUID. Give this or `name`.",
			},
			"name": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Exact API name. Fails when more than one API carries it, rather than picking one silently.",
			},
			"context_path": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Effective context path.",
			},
			"state": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "`STARTED` or `STOPPED`.",
			},
			"plan_ids": schema.MapAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Map of plan name to UUID, covering published, staging and deprecated plans.",
			},
		},
	}
}

func (d *apiDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *apiDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m apiDataSourceModel
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

	var st *client.APIState
	var err error
	if temID {
		st, err = d.c.GetAPIState(ctx, m.ID.ValueString())
	} else {
		st, err = d.c.AchaAPIPorNome(ctx, m.Name.ValueString())
	}
	if err != nil {
		resp.Diagnostics.AddError("looking up the API", err.Error())
		return
	}

	planos, err := d.c.ListPlans(ctx, st.ID)
	if err != nil {
		resp.Diagnostics.AddError("listing the API's plans", err.Error())
		return
	}
	mapa := map[string]string{}
	for _, p := range planos {
		mapa[p.Name] = p.ID
	}
	pm, d2 := types.MapValueFrom(ctx, types.StringType, mapa)
	resp.Diagnostics.Append(d2...)
	if resp.Diagnostics.HasError() {
		return
	}

	m.ID = types.StringValue(st.ID)
	m.Name = types.StringValue(st.Name)
	m.ContextPath = types.StringValue(st.ContextPath)
	m.State = types.StringValue(st.State)
	m.PlanIDs = pm
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
