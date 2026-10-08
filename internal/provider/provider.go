package provider

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/klinux/terraform-provider-gravitee/internal/client"
)

type graviteeProvider struct {
	version string
}

func New(version string) func() provider.Provider {
	return func() provider.Provider { return &graviteeProvider{version: version} }
}

func (p *graviteeProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "gravitee"
	resp.Version = p.version
}

type providerModel struct {
	Endpoint     types.String `tfsdk:"endpoint"`
	Token        types.String `tfsdk:"token"`
	Organization types.String `tfsdk:"organization"`
	Environment  types.String `tfsdk:"environment"`
	Timeout      types.Int64  `tfsdk:"timeout_seconds"`
}

func (p *graviteeProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages Gravitee APIM 3.x objects through the Management API.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Base of the Management API, for example `https://apim.example.com/management`. Can also come from `GRAVITEE_ENDPOINT`.",
			},
			"token": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Bearer token for the Management API. Prefer `GRAVITEE_TOKEN` over putting it in your `.tf`.",
			},
			"organization": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Organization. Defaults to `DEFAULT`.",
			},
			"environment": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Environment. Defaults to `DEFAULT`.",
			},
			"timeout_seconds": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Timeout per HTTP call, in seconds. Defaults to 60.",
			},
		},
	}
}

func (p *graviteeProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// variavel de ambiente perde para valor explicito no .tf
	endpoint := firstNonEmpty(cfg.Endpoint.ValueString(), os.Getenv("GRAVITEE_ENDPOINT"))
	token := firstNonEmpty(cfg.Token.ValueString(), os.Getenv("GRAVITEE_TOKEN"))
	org := firstNonEmpty(cfg.Organization.ValueString(), os.Getenv("GRAVITEE_ORGANIZATION"), "DEFAULT")
	env := firstNonEmpty(cfg.Environment.ValueString(), os.Getenv("GRAVITEE_ENVIRONMENT"), "DEFAULT")

	if endpoint == "" {
		resp.Diagnostics.AddAttributeError(path.Root("endpoint"), "endpoint nao informado",
			"Defina `endpoint` no bloco do provider ou a variavel GRAVITEE_ENDPOINT.")
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "token nao informado",
			"Defina `token` no bloco do provider ou a variavel GRAVITEE_TOKEN.")
	}
	if !strings.HasSuffix(strings.TrimRight(endpoint, "/"), "/management") && endpoint != "" {
		resp.Diagnostics.AddAttributeWarning(path.Root("endpoint"), "endpoint nao termina em /management",
			"A Management API do APIM 3.x fica sob /management. Confira se o endpoint esta completo.")
	}
	if resp.Diagnostics.HasError() {
		return
	}

	to := 60 * time.Second
	if !cfg.Timeout.IsNull() && cfg.Timeout.ValueInt64() > 0 {
		to = time.Duration(cfg.Timeout.ValueInt64()) * time.Second
	}

	c := client.New(endpoint, org, env, token, to)
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *graviteeProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewApplicationResource,
		NewSubscriptionResource,
		NewAPIResource,
	}
}

func (p *graviteeProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewAPIDataSource,
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
