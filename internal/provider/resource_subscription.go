package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/klinux/terraform-provider-gravitee/internal/client"
)

type subscriptionResource struct {
	c *client.Client
}

func NewSubscriptionResource() resource.Resource { return &subscriptionResource{} }

type subscriptionModel struct {
	ID            types.String `tfsdk:"id"`
	ApplicationID types.String `tfsdk:"application_id"`
	PlanID        types.String `tfsdk:"plan_id"`
	APIID         types.String `tfsdk:"api_id"`
	Status        types.String `tfsdk:"status"`
	ClientID      types.String `tfsdk:"client_id"`
}

func (r *subscriptionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_subscription"
}

func (r *subscriptionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Subscribes an application to a plan. The API has no subscription update: changing `plan_id` or `application_id` forces replacement, and replacing one briefly cuts the consumer's access.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"application_id": schema.StringAttribute{
				Required: true,
				// nao existe PUT de subscription: trocar de application e recriar
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				MarkdownDescription: "Application UUID.",
			},
			"plan_id": schema.StringAttribute{
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				MarkdownDescription: "Plan UUID. With `validation: AUTO` the subscription is created already `ACCEPTED`.",
			},
			"api_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The API that owns the plan, filled in by the server.",
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "`ACCEPTED` when the plan validates automatically; `PENDING` when it requires manual approval.",
			},
			"client_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The application's `client_id`, echoed by the server on the subscription.",
			},
		},
	}
}

func (r *subscriptionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *subscriptionResource) refletir(m *subscriptionModel, out *client.Subscription) {
	m.ID = types.StringValue(out.ID)
	m.APIID = types.StringValue(out.API.ID)
	m.Status = types.StringValue(out.Status)
	if out.ClientID != "" {
		m.ClientID = types.StringValue(out.ClientID)
	} else {
		m.ClientID = types.StringNull()
	}
}

func (r *subscriptionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m subscriptionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	app, plano := m.ApplicationID.ValueString(), m.PlanID.ValueString()

	// uma subscription ativa para o mesmo par ja existente faz o POST falhar.
	// Adotar em vez de estourar deixa o apply idempotente sobre o que foi criado
	// a mao antes do Terraform entrar.
	if ja, err := r.c.FindSubscription(ctx, app, plano); err == nil && ja != nil {
		r.refletir(&m, ja)
		resp.Diagnostics.AddWarning("existing subscription adopted",
			fmt.Sprintf("an active subscription (%s) of application %s to plan %s already existed; it was adopted instead of recreated.", ja.ID, app, plano))
		resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
		return
	}

	out, err := r.c.CreateSubscription(ctx, app, plano)
	if err != nil {
		resp.Diagnostics.AddError("creating the subscription", err.Error())
		return
	}
	lido, err := r.c.GetSubscription(ctx, app, out.ID)
	if err != nil {
		resp.Diagnostics.AddError("reading back the newly created subscription",
			fmt.Sprintf("subscription %s was created but could not be read back: %s", out.ID, err))
		return
	}
	if lido.Plan.ID != plano {
		resp.Diagnostics.AddError("the server subscribed to a different plan",
			fmt.Sprintf("asked for plan %s, but subscription %s landed on plan %s", plano, lido.ID, lido.Plan.ID))
		return
	}
	if client.SubscriptionGone(lido) {
		resp.Diagnostics.AddError("subscription was created already closed",
			fmt.Sprintf("subscription %s came back with status %s", lido.ID, lido.Status))
		return
	}
	if lido.Status == "PENDING" {
		resp.Diagnostics.AddWarning("subscription pending approval",
			fmt.Sprintf("subscription %s is PENDING: plan %s requires manual validation, and access only works once approved.", lido.ID, plano))
	}
	r.refletir(&m, lido)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *subscriptionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m subscriptionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.c.GetSubscription(ctx, m.ApplicationID.ValueString(), m.ID.ValueString())
	if err != nil {
		if client.NotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("reading the subscription", err.Error())
		return
	}
	// DELETE e "close": o registro sobrevive com CLOSED. Tratar como ausente e o
	// que faz destroy seguido de apply convergir.
	if client.SubscriptionGone(out) {
		resp.State.RemoveResource(ctx)
		return
	}
	// o GET de detalhe devolve `application: null`; so sobrescreve se veio algo
	if out.Plan.ID != "" {
		m.PlanID = types.StringValue(out.Plan.ID)
	}
	if out.Application.ID != "" {
		m.ApplicationID = types.StringValue(out.Application.ID)
	}
	r.refletir(&m, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// Update nunca e chamado: os dois campos mutaveis forcam replace. Fica aqui
// porque a interface exige, e grita se algum dia alguem tirar o RequiresReplace.
func (r *subscriptionResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("subscriptions have no update",
		"The Management API exposes no subscription update. Every mutable attribute should force replacement; if you are seeing this, a RequiresReplace was removed from the schema.")
}

func (r *subscriptionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m subscriptionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.CloseSubscription(ctx, m.ApplicationID.ValueString(), m.ID.ValueString()); err != nil {
		if client.NotFound(err) {
			return
		}
		resp.Diagnostics.AddError("closing the subscription", err.Error())
	}
}

// ImportState aceita "<application_id>:<plan_id>", que e como a gente identifica
// uma subscription na pratica, e tambem "<application_id>:<subscription_id>".
func (r *subscriptionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	partes := strings.Split(req.ID, ":")
	if len(partes) != 2 || partes[0] == "" || partes[1] == "" {
		resp.Diagnostics.AddError("invalid import id",
			fmt.Sprintf("expected \"<application_id>:<plan_id>\" or \"<application_id>:<subscription_id>\", got %q", req.ID))
		return
	}
	app, segundo := partes[0], partes[1]

	if s, err := r.c.GetSubscription(ctx, app, segundo); err == nil && !client.SubscriptionGone(s) {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), s.ID)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("application_id"), app)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("plan_id"), s.Plan.ID)...)
		return
	}
	s, err := r.c.FindSubscription(ctx, app, segundo)
	if err != nil {
		resp.Diagnostics.AddError("looking up the subscription to import", err.Error())
		return
	}
	if s == nil {
		resp.Diagnostics.AddError("subscription not found",
			fmt.Sprintf("application %s has no active subscription whose id or plan is %s", app, segundo))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), s.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("application_id"), app)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("plan_id"), s.Plan.ID)...)
}
