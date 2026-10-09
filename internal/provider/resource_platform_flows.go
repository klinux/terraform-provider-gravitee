package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/klinux/terraform-provider-gravitee/internal/client"
)

type platformFlowsResource struct {
	c *client.Client
}

func NewPlatformFlowsResource() resource.Resource { return &platformFlowsResource{} }

type platformFlowsModel struct {
	ID             types.String         `tfsdk:"id"`
	Flows          jsontypes.Normalized `tfsdk:"flows"`
	FlowMode       types.String         `tfsdk:"flow_mode"`
	ClearOnDestroy types.Bool           `tfsdk:"clear_on_destroy"`
}

func (r *platformFlowsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_platform_flows"
}

func (r *platformFlowsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The organization's platform flows, which run for **every API on the gateway**. There is one set per organization, so this resource adopts what is already there rather than creating anything. The flows use the same shape as an API's v2 flows, and are passed through as opaque JSON.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "The organization's id.",
			},
			"flows": schema.StringAttribute{
				Required:            true,
				CustomType:          jsontypes.NormalizedType{},
				PlanModifiers:       []planmodifier.String{definicaoSemantica{}},
				MarkdownDescription: "A JSON array of flows, in order. Order is significant: under `flow_mode = \"DEFAULT\"` every matching flow runs, in the order given. Compared semantically, so whitespace and key order produce no diff.",
			},
			"flow_mode": schema.StringAttribute{
				Optional: true,
				Computed: true,
				// without this the plan marks it unknown whenever the
				// configuration leaves it out, which shows as a diff on a
				// resource that is otherwise unchanged
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "`DEFAULT` runs every matching flow; `BEST_MATCH` runs only the closest match. Left as it stands on the server when not declared.",
			},
			"clear_on_destroy": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "What `terraform destroy` does. With `false` (the default) the resource leaves the gateway untouched and only drops out of state, because removing platform flows changes behaviour for every API at once and should not be a side effect of removing a resource block. Set it to `true` to have destroy empty the flows.",
			},
		},
	}
}

func (r *platformFlowsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("unexpected ProviderData", fmt.Sprintf("expected *client.Client, got %T", req.ProviderData))
		return
	}
	r.c = c
}

func parseFlows(s string) ([]any, error) {
	var f []any
	if err := json.Unmarshal([]byte(s), &f); err != nil {
		return nil, fmt.Errorf("flows is not a JSON array: %w", err)
	}
	return f, nil
}

// ValidateConfig rejects a malformed flow list during plan rather than at
// apply, because an apply here touches every API on the gateway.
func (r *platformFlowsResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m platformFlowsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.Flows.IsNull() || m.Flows.IsUnknown() {
		return
	}
	if _, err := parseFlows(m.Flows.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("flows"), "invalid flows", err.Error())
		return
	}
	if fm := m.FlowMode.ValueString(); fm != "" && fm != "DEFAULT" && fm != "BEST_MATCH" {
		resp.Diagnostics.AddAttributeError(path.Root("flow_mode"), "invalid flow_mode",
			fmt.Sprintf("expected DEFAULT or BEST_MATCH, got %q", fm))
	}
}

// aplica writes flows and flow_mode while preserving everything else on the
// organization. The PUT takes the whole entity, so omitting `name`,
// `description` or `hrids` would clear them.
func (r *platformFlowsResource) aplica(ctx context.Context, m *platformFlowsModel) ([]any, *client.Organizacao, error) {
	desejados, err := parseFlows(m.Flows.ValueString())
	if err != nil {
		return nil, nil, err
	}
	atual, err := r.c.GetOrganization(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("reading the organization: %w", err)
	}
	enviar := *atual
	enviar.Flows = desejados
	if fm := m.FlowMode.ValueString(); fm != "" && !m.FlowMode.IsUnknown() {
		enviar.FlowMode = fm
	}
	if err := r.c.UpdateOrganization(ctx, enviar); err != nil {
		return nil, nil, fmt.Errorf("updating the organization: %w", err)
	}
	// the PUT answers 204 with no body, so the only way to know what landed is
	// to read it back
	depois, err := r.c.GetOrganization(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("reading the organization back: %w", err)
	}
	return desejados, depois, nil
}

// refletir fills the computed attributes. It deliberately leaves `flows`
// alone: that attribute is Required, so Create and Update have to hand back
// exactly the planned value or Terraform rejects the result. The server
// reformats a policy's configuration on the way back, so echoing its version
// would never match. Drift is detected in Read instead.
func (r *platformFlowsResource) refletir(m *platformFlowsModel, org *client.Organizacao) {
	m.ID = types.StringValue(org.ID)
	m.FlowMode = types.StringValue(org.FlowMode)
}

func (r *platformFlowsResource) escreve(ctx context.Context, m *platformFlowsModel, diags *diagSink) {
	desejados, depois, err := r.aplica(ctx, m)
	if err != nil {
		diags.erro("applying the platform flows", err.Error())
		return
	}
	if dif := subconjunto("flows", desejados, depois.Flows); len(dif) > 0 {
		diags.erro("the server did not apply what was sent",
			fmt.Sprintf("the platform flows differ from what was declared:\n  - %s", join(dif, "\n  - ")))
		return
	}
	r.refletir(m, depois)
}

// diagSink lets Create and Update share escreve without duplicating the
// framework's two different response types.
type diagSink struct{ add func(resumo, detalhe string) }

func (d *diagSink) erro(resumo, detalhe string) { d.add(resumo, detalhe) }

func (r *platformFlowsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m platformFlowsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.escreve(ctx, &m, &diagSink{add: resp.Diagnostics.AddError})
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddWarning("platform flows now apply to every API",
		"These flows run for every API served by this gateway. A mistake here is not scoped to one API.")
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *platformFlowsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m platformFlowsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	org, err := r.c.GetOrganization(ctx)
	if err != nil {
		if client.NotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("reading the organization", err.Error())
		return
	}
	noServidor, err := json.Marshal(org.Flows)
	if err != nil {
		resp.Diagnostics.AddError("serializing the flows", err.Error())
		return
	}
	r.refletir(&m, org)

	// `flows` is only rewritten when the server really diverged. Always
	// echoing the server's rendering would show a diff forever, because it
	// reformats a policy's configuration on the way back.
	anterior := m.Flows.ValueString()
	if anterior == "" || !mesmoJSONTexto(anterior, string(noServidor)) {
		m.Flows = jsontypes.NewNormalizedValue(string(noServidor))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *platformFlowsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m platformFlowsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.escreve(ctx, &m, &diagSink{add: resp.Diagnostics.AddError})
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *platformFlowsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m platformFlowsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !m.ClearOnDestroy.ValueBool() {
		resp.Diagnostics.AddWarning("platform flows left in place",
			"The resource was removed from state, but the flows on the gateway were not touched. Emptying them would change behaviour for every API at once, so it is not done as a side effect of a destroy. Set `clear_on_destroy = true` if you want destroy to empty them.")
		return
	}
	atual, err := r.c.GetOrganization(ctx)
	if err != nil {
		if client.NotFound(err) {
			return
		}
		resp.Diagnostics.AddError("reading the organization before clearing the flows", err.Error())
		return
	}
	enviar := *atual
	enviar.Flows = []any{}
	if err := r.c.UpdateOrganization(ctx, enviar); err != nil {
		resp.Diagnostics.AddError("clearing the platform flows", err.Error())
		return
	}
	resp.Diagnostics.AddWarning("platform flows emptied",
		"`clear_on_destroy` was set, so every platform flow was removed. This changes behaviour for every API on the gateway.")
}

func (r *platformFlowsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// there is one set per organization; the id is the organization's
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("clear_on_destroy"), false)...)
}
