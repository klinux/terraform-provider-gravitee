package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/klinux/terraform-provider-gravitee/internal/client"
)

type apiResource struct {
	c *client.Client
}

func NewAPIResource() resource.Resource { return &apiResource{} }

type apiModel struct {
	ID          types.String         `tfsdk:"id"`
	Definition  jsontypes.Normalized `tfsdk:"definition"`
	Name        types.String         `tfsdk:"name"`
	ContextPath types.String         `tfsdk:"context_path"`
	State       types.String         `tfsdk:"state"`
	Deploy      types.Bool           `tfsdk:"deploy"`
	AllowPlanRm types.Bool           `tfsdk:"allow_plan_deletion"`
	PlanIDs     types.Map            `tfsdk:"plan_ids"`
}

func (r *apiResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api"
}

func (r *apiResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An API with a `2.0.0` (flows) definition. The provider does not interpret policies: the definition is opaque JSON, identical in shape to what `GET /apis/{id}/export` returns. `1.0.0` (paths) definitions are not supported.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"definition": schema.StringAttribute{
				Required:   true,
				CustomType: jsontypes.NormalizedType{},
				PlanModifiers: []planmodifier.String{
					definicaoSemantica{},
				},
				MarkdownDescription: "The API definition as JSON. Compared semantically, so key order and whitespace produce no diff. Fields the server owns (`id`, `primaryOwner`, `members`, `pages`, and each plan's `id`/`created_at`/`updated_at`/`order`) are stripped before comparing. A top-level key this definition does not declare is preserved as it stands on the server, not removed.",
			},
			"name": schema.StringAttribute{
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "Name read back from the applied definition.",
			},
			"context_path": schema.StringAttribute{
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "Effective context path, read from the server.",
			},
			"state": schema.StringAttribute{
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "`STARTED` or `STOPPED`.",
			},
			"deploy": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				MarkdownDescription: "Calls `POST /apis/{id}/deploy` after writing, when the gateway is out of sync. Needed because changing a plan does not bump the API's `updated_at`, so the gateway never reloads on its own.",
			},
			"allow_plan_deletion": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Allows an apply to delete an existing plan that is absent from the definition. **Deleting a plan deletes its subscriptions.** With `false` (the default) the apply fails and lists what would be lost.",
			},
			"plan_ids": schema.MapAttribute{
				Computed:    true,
				ElementType: types.StringType,
				// Sem isto, qualquer update da API deixa plan_ids desconhecido no
				// plan, e isso propaga para gravitee_subscription.plan_id, que
				// forca replace -- ou seja, um update cosmetico derrubaria a
				// subscription do parceiro. Os ids sao estaveis entre updates
				// justamente porque PareiaPlanos reenvia os ids existentes.
				PlanModifiers:       []planmodifier.Map{mapplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "Map of plan name to UUID, filled in by the server. Use it for `gravitee_subscription.plan_id` instead of hardcoding plan UUIDs.",
			},
		},
	}
}

// ValidateConfig recusa definicao invalida no `plan`, nao no `apply`.
//
// Sem isto, uma definicao em "1.0.0" ou com JSON quebrado so estouraria na hora
// de escrever -- e no caso do update o import nao e atomico, entao parte da
// definicao poderia ja ter sido aplicada antes da recusa.
func (r *apiResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m apiModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.Definition.IsNull() || m.Definition.IsUnknown() {
		return
	}
	if _, err := parseDefinicao(m.Definition.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("definition"), "invalid definition", err.Error())
	}
}

func (r *apiResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// parse le o JSON do atributo e recusa o que o provider nao suporta.
func parseDefinicao(s string) (client.APIRaw, error) {
	var def client.APIRaw
	if err := json.Unmarshal([]byte(s), &def); err != nil {
		return nil, fmt.Errorf("definition is not valid JSON: %w", err)
	}
	ver, _ := def["gravitee"].(string)
	switch ver {
	case "2.0.0":
	case "":
		return nil, fmt.Errorf("definition has no `gravitee` field; this resource only accepts a \"2.0.0\" definition")
	case "1.0.0":
		return nil, fmt.Errorf("definition is \"1.0.0\" (paths). This resource only accepts \"2.0.0\" (flows): in v1 the policy id is the object key, a shape the provider does not model. Migrate the API to flows before bringing it into Terraform")
	default:
		return nil, fmt.Errorf("definition has gravitee=%q; this resource only accepts \"2.0.0\"", ver)
	}
	return def, nil
}

// recorta projeta o aplicado na forma que o desejado declara, recursivamente:
// para cada chave declarada pega o valor do servidor, e descarta o que nao foi
// declarado.
//
// O recorte tem de ser recursivo, nao so de topo: o APIM adiciona campo dentro
// de plano (`comment_required`, `type`) e dentro de step (`description`), e sem
// descer esses campos entrariam no state e dariam diff eterno contra a config.
func recorta(aplicado, desejado client.APIRaw) client.APIRaw {
	v := recortaValor(map[string]any(aplicado), map[string]any(desejado))
	if m, ok := v.(map[string]any); ok {
		return client.APIRaw(m)
	}
	return client.APIRaw{}
}

func recortaValor(aplicado, desejado any) any {
	switch d := desejado.(type) {
	case map[string]any:
		a, ok := aplicado.(map[string]any)
		if !ok {
			return aplicado
		}
		out := make(map[string]any, len(d))
		for k := range d {
			if av, existe := a[k]; existe {
				out[k] = recortaValor(av, d[k])
			}
		}
		return out
	case []any:
		a, ok := aplicado.([]any)
		if !ok {
			return aplicado
		}
		out := make([]any, 0, len(a))
		for i, av := range a {
			if i < len(d) {
				out = append(out, recortaValor(av, d[i]))
			} else {
				out = append(out, av)
			}
		}
		return out
	default:
		return aplicado
	}
}

// mesmoJSON compara duas definicoes ignorando ordem de chaves.
func mesmoJSON(a, b client.APIRaw) (bool, error) {
	ja, err := json.Marshal(normaliza(map[string]any(a)))
	if err != nil {
		return false, err
	}
	jb, err := json.Marshal(normaliza(map[string]any(b)))
	if err != nil {
		return false, err
	}
	return string(ja) == string(jb), nil
}

func canonico(def client.APIRaw) (string, error) {
	b, err := json.Marshal(def)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func planIDs(ctx context.Context, aplicado client.APIRaw) types.Map {
	m := map[string]string{}
	if planos, ok := aplicado["plans"].([]any); ok {
		for _, p := range planos {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			nome, _ := pm["name"].(string)
			id, _ := pm["id"].(string)
			if nome != "" && id != "" {
				m[nome] = id
			}
		}
	}
	v, _ := types.MapValueFrom(ctx, types.StringType, m)
	return v
}

// conferirAPI verifica que tudo o que foi enviado esta presente e igual no que
// voltou do servidor.
//
// A comparacao e por SUBCONJUNTO, nao por igualdade: o APIM preenche defaults
// que nao foram enviados (um plano volta com `comment_required: false` e
// `type: "API"`, por exemplo), e isso nao e divergencia. O que importa e o
// contrario -- campo enviado que o servidor ignorou. A Management API aceita
// campo que nao entende e responde 200 sem aplicar nada: foi assim que
// `selectionRule` em camelCase virou no-op silencioso.
func conferirAPI(desejado, aplicado client.APIRaw) []string {
	// o export devolve os planos em ordem propria: alinhar por nome antes, senao
	// toda API com mais de um plano acusaria divergencia falsa
	alinhado, faltando := client.AlinhaPlanos(desejado, aplicado)
	var dif []string
	for _, nome := range faltando {
		dif = append(dif, fmt.Sprintf("plans: plan %q was sent and does not exist on the applied API", nome))
	}
	// a conversao explicita e necessaria: client.APIRaw e tipo nomeado e o type
	// switch de subconjunto nao casaria com `case map[string]any`
	dif = append(dif, subconjunto("",
		map[string]any(client.LimpaCamposDoServidor(desejado)),
		map[string]any(client.LimpaCamposDoServidor(alinhado)))...)
	return dif
}

// subconjunto desce recursivamente e reporta todo ponto em que `quis` nao esta
// refletido em `tem`. A ordem de listas importa: ordem de policy e semantica.
func subconjunto(onde string, quis, tem any) []string {
	var dif []string
	switch q := quis.(type) {
	case map[string]any:
		t, ok := tem.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: sent an object, applied %s", rotulo(onde), tipo(tem))}
		}
		var chaves []string
		for k := range q {
			chaves = append(chaves, k)
		}
		sort.Strings(chaves)
		for _, k := range chaves {
			tv, existe := t[k]
			if !existe {
				dif = append(dif, fmt.Sprintf("%s: sent, but the server did not keep it (value %s)",
					rotulo(junta(onde, k)), corta(jsonDe(q[k]))))
				continue
			}
			dif = append(dif, subconjunto(junta(onde, k), q[k], tv)...)
		}
	case []any:
		t, ok := tem.([]any)
		if !ok {
			return []string{fmt.Sprintf("%s: sent a list, applied %s", rotulo(onde), tipo(tem))}
		}
		if len(q) != len(t) {
			return []string{fmt.Sprintf("%s: sent %d item(s), applied %d", rotulo(onde), len(q), len(t))}
		}
		for i := range q {
			dif = append(dif, subconjunto(fmt.Sprintf("%s[%d]", onde, i), q[i], t[i])...)
		}
	default:
		if jsonDe(quis) != jsonDe(tem) {
			dif = append(dif, fmt.Sprintf("%s: sent %s, applied %s",
				rotulo(onde), corta(jsonDe(quis)), corta(jsonDe(tem))))
		}
	}
	return dif
}

func junta(a, b string) string {
	if a == "" {
		return b
	}
	return a + "." + b
}

func rotulo(s string) string {
	if s == "" {
		return "(raiz)"
	}
	return s
}

func tipo(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "objeto"
	case []any:
		return "lista"
	default:
		return corta(jsonDe(v))
	}
}

func jsonDe(v any) string {
	b, err := json.Marshal(normaliza(v))
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// normaliza ordena chaves de mapa recursivamente para a comparacao nao depender
// da ordem em que o servidor serializa.
func normaliza(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[k] = normaliza(vv)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, vv := range t {
			out[i] = normaliza(vv)
		}
		return out
	default:
		return v
	}
}

func corta(s string) string {
	if len(s) > 240 {
		return s[:240] + "..."
	}
	return s
}

// sincroniza chama o deploy se o gateway ainda nao tem a definicao corrente.
func (r *apiResource) sincroniza(ctx context.Context, id string, quer bool) (bool, error) {
	if !quer {
		return false, nil
	}
	ok, err := r.c.Sincronizada(ctx, id)
	if err != nil {
		return false, err
	}
	if ok {
		return false, nil
	}
	if err := r.c.DeployAPI(ctx, id); err != nil {
		return false, err
	}
	return true, nil
}

func (r *apiResource) refletir(ctx context.Context, m *apiModel, aplicado client.APIRaw, desejado client.APIRaw) error {
	id, _ := aplicado["id"].(string)
	m.ID = types.StringValue(id)
	if n, ok := aplicado["name"].(string); ok {
		m.Name = types.StringValue(n)
	}
	m.PlanIDs = planIDs(ctx, aplicado)

	st, err := r.c.GetAPIState(ctx, id)
	if err == nil {
		m.ContextPath = types.StringValue(st.ContextPath)
		m.State = types.StringValue(st.State)
	} else {
		m.ContextPath = types.StringNull()
		m.State = types.StringNull()
	}

	return nil
}

// projeta devolve o estado do servidor recortado na forma que o desejado
// declara, ja sem os campos que o APIM possui. E o lado "real" da comparacao de
// drift.
func projeta(aplicado, desejado client.APIRaw) client.APIRaw {
	alinhado, _ := client.AlinhaPlanos(desejado, aplicado)
	out := recorta(client.LimpaCamposDoServidor(alinhado), desejado)
	devolveOrdemDeclarada(out, desejado)
	return out
}

// devolveOrdemDeclarada repoe no projetado o `order` que o usuario declarou.
//
// O import nao honra `order`: o servidor zera. Se o projetado trouxesse o valor
// do servidor, quem declarou `order` ficaria com diff que nunca converge. O
// aviso emitido no apply e que informa que o declarado nao pegou; o state
// acompanha o declarado para o plan parar de oscilar.
func devolveOrdemDeclarada(projetado, desejado client.APIRaw) {
	pp, ok1 := projetado["plans"].([]any)
	dp, ok2 := desejado["plans"].([]any)
	if !ok1 || !ok2 {
		return
	}
	for i := range pp {
		if i >= len(dp) {
			break
		}
		pm, ok1 := pp[i].(map[string]any)
		dm, ok2 := dp[i].(map[string]any)
		if !ok1 || !ok2 {
			continue
		}
		if o, declarou := dm["order"]; declarou {
			pm["order"] = o
		}
	}
}

func (r *apiResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m apiModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	desejado, err := parseDefinicao(m.Definition.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("definition"), "invalid definition", err.Error())
		return
	}
	enviado := client.LimpaCamposDoServidor(desejado)

	criado, err := r.c.CreateAPI(ctx, enviado)
	if err != nil {
		resp.Diagnostics.AddError("creating the API", err.Error())
		return
	}
	id, _ := criado["id"].(string)
	if id == "" {
		resp.Diagnostics.AddError("creating the API", "the server returned no id in the import response")
		return
	}
	aplicado, err := r.c.ExportAPI(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("reading back the newly created API",
			fmt.Sprintf("API %s was created but could not be exported back. It exists on the server and is not in state: delete it by hand or import it. %s", id, err))
		return
	}
	// A API ja existe no servidor. O state tem de sair completo mesmo quando a
	// verificacao falha, senao o Terraform recusa o resultado ("inconsistent
	// result after apply") e o objeto fica orfao, fora do state.
	if err := r.refletir(ctx, &m, aplicado, enviado); err != nil {
		resp.Diagnostics.AddError("building state", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	if dif := conferirAPI(enviado, aplicado); len(dif) > 0 {
		resp.Diagnostics.AddError("the server did not apply what was sent",
			fmt.Sprintf("API %s was created but differs from what was declared:\n  - %s\n\nThe API is in state: fix the definition and apply again, or destroy it.",
				id, join(dif, "\n  - ")))
		return
	}
	if fora := client.OrdemDeclaradaIgnorada(enviado, aplicado); len(fora) > 0 {
		resp.Diagnostics.AddWarning("the import does not honour a plan's `order`",
			fmt.Sprintf("the declared order was not applied to: %s.\n\nThe import zeroes each plan's `order`. If the order matters, set it through the plan endpoint afterwards, or drop the field from the definition so it does not imply otherwise.", join(fora, "; ")))
	}
	if fez, err := r.sincroniza(ctx, id, m.Deploy.ValueBool()); err != nil {
		resp.Diagnostics.AddError("deploying the API", err.Error())
		return
	} else if fez {
		resp.Diagnostics.AddWarning("deploy triggered",
			fmt.Sprintf("API %s was deployed to the gateways after being created.", id))
	}
	if err := r.refletir(ctx, &m, aplicado, enviado); err != nil {
		resp.Diagnostics.AddError("building state", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *apiResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m apiModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := m.ID.ValueString()
	aplicado, err := r.c.ExportAPI(ctx, id)
	if err != nil {
		if client.NotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("exporting the API", err.Error())
		return
	}
	// num import o state so tem o id: ai o desejado e a propria definicao do
	// servidor, e nada e recortado
	vindoDoState := m.Definition.ValueString()
	novoNoState := false
	desejado := client.APIRaw{}
	if vindoDoState != "" {
		d, err := parseDefinicao(vindoDoState)
		if err != nil {
			resp.Diagnostics.AddError("the definition in state is unusable", err.Error())
			return
		}
		desejado = client.LimpaCamposDoServidor(d)
	} else {
		desejado = client.LimpaCamposDoServidor(aplicado)
		novoNoState = true
	}

	if err := r.refletir(ctx, &m, aplicado, desejado); err != nil {
		resp.Diagnostics.AddError("building state", err.Error())
		return
	}

	// O definition so e reescrito quando o servidor divergiu de fato. Reescrever
	// sempre com a forma canonica produziria diff eterno nos campos que o import
	// nao honra (o `order` de plano, por exemplo, que volta zerado).
	projetado := projeta(aplicado, desejado)
	igual, err := mesmoJSON(desejado, projetado)
	if err != nil {
		resp.Diagnostics.AddError("comparing the definition", err.Error())
		return
	}
	if novoNoState || !igual {
		c, err := canonico(projetado)
		if err != nil {
			resp.Diagnostics.AddError("serializing the definition", err.Error())
			return
		}
		m.Definition = jsontypes.NewNormalizedValue(c)
	} else {
		m.Definition = jsontypes.NewNormalizedValue(vindoDoState)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *apiResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m, estado apiModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &estado)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := estado.ID.ValueString()
	desejado, err := parseDefinicao(m.Definition.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("definition"), "invalid definition", err.Error())
		return
	}

	// Se so mudaram flags do provider (`deploy`, `allow_plan_deletion`), nao ha
	// o que escrever. Vale pular: o import nao e atomico, entao toda escrita
	// evitada e risco evitado -- e na adocao da frota isso poupa 206 imports
	// que nao mudariam nada.
	if !estado.Definition.IsNull() && mesmoJSONTexto(estado.Definition.ValueString(), m.Definition.ValueString()) {
		aplicado, err := r.c.ExportAPI(ctx, id)
		if err != nil {
			resp.Diagnostics.AddError("exporting the API", err.Error())
			return
		}
		m.ID = types.StringValue(id)
		limpo := client.LimpaCamposDoServidor(desejado)
		if err := r.refletir(ctx, &m, aplicado, limpo); err != nil {
			resp.Diagnostics.AddError("building state", err.Error())
			return
		}
		if fez, err := r.sincroniza(ctx, id, m.Deploy.ValueBool()); err != nil {
			resp.Diagnostics.AddError("deploying the API", err.Error())
			return
		} else if fez {
			resp.Diagnostics.AddWarning("deploy triggered",
				fmt.Sprintf("the definition of API %s did not change, but the gateway was out of sync and has been updated.", id))
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
		return
	}

	atual, err := r.c.ExportAPI(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("exporting the API before the update", err.Error())
		return
	}

	// Semantica de merge: chave de topo que a definition NAO declara e
	// preservada como esta no servidor, nao removida.
	//
	// Sem isso uma definition parcial seria destrutiva: omitir `resources`
	// apagaria o oauth2-keycloak-resource da API e quebraria o plano OAuth2.
	// A leitura ja trata chave nao declarada como nao gerenciada (ver recorta);
	// a escrita tem de combinar com isso.
	declarado := client.LimpaCamposDoServidor(desejado)
	mesclado := client.LimpaCamposDoServidor(atual)
	for k, v := range declarado {
		mesclado[k] = v
	}

	// o import pareia planos por id e apaga o ausente. Sem reenviar os ids, cada
	// update recriaria os planos e levaria as subscriptions com eles.
	comIDs, descartados, err := client.PareiaPlanos(mesclado, atual)
	if err != nil {
		resp.Diagnostics.AddError("pairing plans", err.Error())
		return
	}
	if len(descartados) > 0 && !m.AllowPlanRm.ValueBool() {
		var linhas []string
		for _, d := range descartados {
			linhas = append(linhas, fmt.Sprintf("%s (%s, id %s)", d.Nome, d.Security, d.ID))
		}
		resp.Diagnostics.AddError("this apply would delete an existing plan",
			fmt.Sprintf("API %s has a plan that the definition does not declare. The import deletes any plan missing from the payload, and deleting a plan deletes its subscriptions:\n  - %s\n\nDeclare the plan in the definition, or set `allow_plan_deletion = true` if the loss is intended.",
				id, join(linhas, "\n  - ")))
		return
	}

	// O import precisa do `id` da propria API no corpo, nao so na URL. Provado
	// contra o stage:
	//
	//   id de topo + plans[].id  -> 200, planos preservados
	//   id de topo, sem plans[].id -> 400 "can't delete a plan with existing subscriptions"
	//   sem id de topo, com plans[].id -> 400, idem
	//
	// Sem os dois o import trata os planos como novos e tenta apagar os antigos.
	comIDs["id"] = id

	if _, err := r.c.UpdateAPI(ctx, id, comIDs); err != nil {
		resp.Diagnostics.AddError("updating the API",
			fmt.Sprintf("%s\n\nWarning: the import is NOT atomic. Part of the definition may have been applied before the failure; run `terraform plan` to see the API's real state (%s).", err, id))
		return
	}
	aplicado, err := r.c.ExportAPI(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("reading the API back after the update", err.Error())
		return
	}
	// confere so o que foi declarado: o que veio do merge e estado do servidor,
	// nao desejo do usuario
	if dif := conferirAPI(declarado, aplicado); len(dif) > 0 {
		resp.Diagnostics.AddError("the server did not apply what was sent",
			fmt.Sprintf("API %s was updated but differs from what was declared:\n  - %s", id, join(dif, "\n  - ")))
		return
	}
	if fora := client.OrdemDeclaradaIgnorada(declarado, aplicado); len(fora) > 0 {
		resp.Diagnostics.AddWarning("the import does not honour a plan's `order`",
			fmt.Sprintf("the declared order was not applied to: %s.\n\nThe import zeroes each plan's `order`. If the order matters, set it through the plan endpoint afterwards, or drop the field from the definition so it does not imply otherwise.", join(fora, "; ")))
	}
	if fez, err := r.sincroniza(ctx, id, m.Deploy.ValueBool()); err != nil {
		resp.Diagnostics.AddError("deploying the API", err.Error())
		return
	} else if fez {
		resp.Diagnostics.AddWarning("deploy triggered",
			fmt.Sprintf("API %s was deployed to the gateways after being updated.", id))
	}
	m.ID = types.StringValue(id)
	if err := r.refletir(ctx, &m, aplicado, declarado); err != nil {
		resp.Diagnostics.AddError("building state", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *apiResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m apiModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := m.ID.ValueString()

	// O DELETE recusa API com plano aberto:
	//   "Plan(s) [...] must be closed before being able to delete the API"
	// Fechar plano fecha as subscriptions dele -- inevitavel para apagar a API.
	planos, err := r.c.ListPlans(ctx, id)
	if err != nil && !client.NotFound(err) {
		resp.Diagnostics.AddError("listing plans before deleting the API", err.Error())
		return
	}
	var fechados []string
	for _, pl := range planos {
		if err := r.c.ClosePlan(ctx, id, pl.ID); err != nil {
			resp.Diagnostics.AddError("closing a plan before deleting the API",
				fmt.Sprintf("plan %s (%s) of API %s: %s", pl.Name, pl.ID, id, err))
			return
		}
		fechados = append(fechados, fmt.Sprintf("%s (%s)", pl.Name, pl.Security))
	}
	if len(fechados) > 0 {
		resp.Diagnostics.AddWarning("plans closed in order to delete the API",
			fmt.Sprintf("API %s had open plans, and the server requires them closed before a delete. Closed, along with their subscriptions: %s",
				id, join(fechados, ", ")))
	}

	// API STARTED tambem pode recusar; para antes de tentar
	_ = r.c.StopAPI(ctx, id)
	if err := r.c.DeleteAPI(ctx, id); err != nil && !client.NotFound(err) {
		resp.Diagnostics.AddError("deleting the API", err.Error())
	}
}

func (r *apiResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
	// as flags de comportamento nao existem no servidor: sem semea-las com o
	// padrao, o primeiro plan depois do import mostraria diff nelas
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("deploy"), true)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("allow_plan_deletion"), false)...)
}
