package provider

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/klinux/terraform-provider-gravitee/internal/client"
)

type applicationResource struct {
	c *client.Client
}

func NewApplicationResource() resource.Resource { return &applicationResource{} }

type applicationModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	AppType     types.String `tfsdk:"app_type"`
	ClientID    types.String `tfsdk:"client_id"`
	Groups      types.Set    `tfsdk:"groups"`
	Status      types.String `tfsdk:"status"`
	Type        types.String `tfsdk:"type"`
}

func (r *applicationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application"
}

func (r *applicationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An APIM application. Its `client_id` is what the gateway matches against the token when the API's OAuth2 plan runs with `modeStrict` enabled.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "UUID assigned by the server.",
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Application name.",
			},
			"description": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Required by the API: `UpdateApplicationEntity` demands `description`.",
			},
			"app_type": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("web"),
				MarkdownDescription: "`settings.app.type`. Defaults to `web`.",
			},
			"client_id": schema.StringAttribute{
				Optional: true,
				// Computed porque o APIM gera um client_id quando nenhum e enviado
				Computed:            true,
				MarkdownDescription: "`settings.app.client_id`. Must match the `client_id` of the client in your identity provider. If omitted, the server generates one.",
			},
			"groups": schema.SetAttribute{
				Optional: true,
				// Computed porque criar application sem groups faz o APIM
				// atribuir um grupo default por conta propria
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "UUIDs of the groups that own the application. If omitted, the server assigns a default group.",
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Computed by the server, for example `ACTIVE`.",
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Internal type computed by the server, for example `SIMPLE`.",
			},
		},
	}
}

func (r *applicationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("ProviderData inesperado", fmt.Sprintf("esperava *client.Client, veio %T", req.ProviderData))
		return
	}
	r.c = c
}

func (m *applicationModel) toAPI(ctx context.Context) (client.Application, error) {
	var grupos []string
	if !m.Groups.IsNull() && !m.Groups.IsUnknown() {
		if d := m.Groups.ElementsAs(ctx, &grupos, false); d.HasError() {
			return client.Application{}, fmt.Errorf("lendo groups")
		}
		sort.Strings(grupos)
	}
	app := client.Application{
		Name:        m.Name.ValueString(),
		Description: m.Description.ValueString(),
		Groups:      grupos,
		Settings: &client.AppSettings{App: &client.SimpleAppSettings{
			Type:     m.AppType.ValueString(),
			ClientID: m.ClientID.ValueString(),
		}},
	}
	return app, nil
}

func (r *applicationResource) refletir(ctx context.Context, m *applicationModel, out *client.Application) {
	m.ID = types.StringValue(out.ID)
	m.Name = types.StringValue(out.Name)
	m.Description = types.StringValue(out.Description)
	m.Status = types.StringValue(out.Status)
	m.Type = types.StringValue(out.Type)
	if out.Settings != nil && out.Settings.App != nil {
		m.AppType = types.StringValue(out.Settings.App.Type)
		if out.Settings.App.ClientID != "" {
			m.ClientID = types.StringValue(out.Settings.App.ClientID)
		} else {
			m.ClientID = types.StringNull()
		}
	}
	if len(out.Groups) == 0 {
		m.Groups = types.SetNull(types.StringType)
		return
	}
	g := append([]string(nil), out.Groups...)
	sort.Strings(g)
	s, _ := types.SetValueFrom(ctx, types.StringType, g)
	m.Groups = s
}

// conferir compara o que foi enviado com o que o servidor devolveu.
//
// A Management API 3.x aceita campo que nao entende e responde 200 sem aplicar
// nada: foi assim que `selectionRule` em camelCase virou no-op silencioso. Entao
// o provider rele e falha o apply em vez de deixar o state mentir.
func conferir(enviado client.Application, voltou *client.Application) []string {
	var dif []string
	if enviado.Name != voltou.Name {
		dif = append(dif, fmt.Sprintf("name: enviado %q, voltou %q", enviado.Name, voltou.Name))
	}
	if enviado.Description != voltou.Description {
		dif = append(dif, fmt.Sprintf("description: enviado %q, voltou %q", enviado.Description, voltou.Description))
	}
	var ea, va client.SimpleAppSettings
	if enviado.Settings != nil && enviado.Settings.App != nil {
		ea = *enviado.Settings.App
	}
	if voltou.Settings != nil && voltou.Settings.App != nil {
		va = *voltou.Settings.App
	}
	if ea.Type != va.Type {
		dif = append(dif, fmt.Sprintf("settings.app.type: enviado %q, voltou %q", ea.Type, va.Type))
	}
	if ea.ClientID != va.ClientID {
		dif = append(dif, fmt.Sprintf("settings.app.client_id: enviado %q, voltou %q", ea.ClientID, va.ClientID))
	}
	// groups so e comparado quando foi declarado: criar application sem groups
	// faz o APIM atribuir um grupo default por conta propria, o que e valor
	// calculado e nao divergencia.
	if len(enviado.Groups) > 0 {
		ev, vv := append([]string(nil), enviado.Groups...), append([]string(nil), voltou.Groups...)
		sort.Strings(ev)
		sort.Strings(vv)
		if fmt.Sprint(ev) != fmt.Sprint(vv) {
			dif = append(dif, fmt.Sprintf("groups: enviado %v, voltou %v", ev, vv))
		}
	}
	return dif
}

func (r *applicationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m applicationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	enviado, err := m.toAPI(ctx)
	if err != nil {
		resp.Diagnostics.AddError("montando corpo", err.Error())
		return
	}
	out, err := r.c.CreateApplication(ctx, enviado)
	if err != nil {
		resp.Diagnostics.AddError("criando application", err.Error())
		return
	}
	// relitura: o POST ja devolve a entidade, mas o GET e o que o Read vai usar
	lido, err := r.c.GetApplication(ctx, out.ID)
	if err != nil {
		resp.Diagnostics.AddError("relendo application recem-criada",
			fmt.Sprintf("a application %s foi criada mas nao pode ser lida de volta: %s", out.ID, err))
		return
	}
	// state completo antes de qualquer erro: a application ja existe, e state
	// parcial faz o Terraform recusar o resultado e deixar o objeto orfao.
	r.refletir(ctx, &m, lido)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	if dif := conferir(enviado, lido); len(dif) > 0 {
		resp.Diagnostics.AddError("o APIM nao aplicou o que foi enviado",
			fmt.Sprintf("application %s criada, mas divergiu do desejado:\n  - %s\n\nEla esta no state: corrija a configuracao e rode apply de novo, ou destroy.",
				out.ID, join(dif, "\n  - ")))
		return
	}
}

func (r *applicationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m applicationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.c.GetApplication(ctx, m.ID.ValueString())
	if err != nil {
		if client.NotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("lendo application", err.Error())
		return
	}
	// application apagada no APIM fica ARCHIVED, nao desaparece
	if out.Status == "ARCHIVED" {
		resp.State.RemoveResource(ctx)
		return
	}
	r.refletir(ctx, &m, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *applicationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m, estado applicationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &estado)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := estado.ID.ValueString()

	// read-modify-write: o PUT exige o corpo inteiro e zera o que nao vier.
	// Preserva settings.oauth, que o provider nao modela.
	atual, err := r.c.GetApplication(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("lendo application antes do update", err.Error())
		return
	}
	enviado, err := m.toAPI(ctx)
	if err != nil {
		resp.Diagnostics.AddError("montando corpo", err.Error())
		return
	}
	if atual.Settings != nil && len(atual.Settings.OAuth) > 0 {
		enviado.Settings.OAuth = atual.Settings.OAuth
	}
	if _, err := r.c.UpdateApplication(ctx, id, enviado); err != nil {
		resp.Diagnostics.AddError("atualizando application", err.Error())
		return
	}
	lido, err := r.c.GetApplication(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("relendo application depois do update", err.Error())
		return
	}
	if dif := conferir(enviado, lido); len(dif) > 0 {
		resp.Diagnostics.AddError("o APIM nao aplicou o que foi enviado",
			fmt.Sprintf("application %s atualizada, mas divergiu do desejado:\n  - %s", id, join(dif, "\n  - ")))
		return
	}
	m.ID = types.StringValue(id)
	r.refletir(ctx, &m, lido)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *applicationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m applicationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.DeleteApplication(ctx, m.ID.ValueString()); err != nil {
		if client.NotFound(err) {
			return
		}
		resp.Diagnostics.AddError("apagando application", err.Error())
	}
}

func (r *applicationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func join(s []string, sep string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += sep
		}
		out += v
	}
	return out
}
