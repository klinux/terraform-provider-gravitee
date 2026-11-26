package provider

import (
	"context"
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// planIDsPorNome planeja `plan_ids` a partir dos nomes de plano que a
// definition declara, mantendo o id do state para plano que ja existe e
// deixando desconhecido o plano novo.
//
// Antes havia aqui um mapplanmodifier.UseStateForUnknown(), que copia o mapa do
// state inteiro para o plan. Isso quebra justamente o apply que ACRESCENTA um
// plano: o plan prometia {Default} e o servidor devolveu {Default, OAuth2}, e o
// Terraform rejeita a diferenca com
//
//	Provider produced inconsistent result after apply
//	.plan_ids: new element "OAuth2" has appeared
//
// Foi o que derrubou o job da partners em producao, depois de a escrita e o
// deploy ja terem acontecido -- o erro vem da conferencia que o core faz
// DEPOIS do apply, entao o estrago e um job vermelho, nao uma API quebrada.
//
// Tirar o modifier tambem nao serve: sem ele plan_ids fica desconhecido em todo
// update, isso propaga para gravitee_subscription.plan_id, e como esse atributo
// forca replace um update cosmetico derrubaria a subscription do parceiro.
//
// Planejar por nome atende aos dois: nome que ja esta no state mantem o id
// (conhecido, logo a subscription nao se move), nome novo entra desconhecido
// (logo o Terraform aceita o id que o servidor atribuir). Mapa com elemento
// desconhecido e valido no framework, e os ids sao estaveis entre updates
// porque PareiaPlanos reenvia os ids existentes.
type planIDsPorNome struct{}

func (planIDsPorNome) Description(_ context.Context) string {
	return "keeps each existing plan's id from state and leaves a plan the definition has just added unknown"
}

func (p planIDsPorNome) MarkdownDescription(ctx context.Context) string {
	return p.Description(ctx)
}

func (planIDsPorNome) PlanModifyMap(ctx context.Context, req planmodifier.MapRequest, resp *planmodifier.MapResponse) {
	// create: nao ha state anterior, todo id sai do apply
	if req.State.Raw.IsNull() {
		return
	}
	// destroy
	if req.Plan.Raw.IsNull() {
		return
	}
	// Plan sem mudanca: o valor ja veio conhecido, nao ha o que planejar.
	if !req.PlanValue.IsUnknown() {
		return
	}
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}

	var def jsontypes.Normalized
	// Os diagnosticos da leitura sao descartados de proposito: definition que
	// nao e JSON valido ja e recusada pelo validador do proprio atributo, e o
	// modifier reportar de novo daria erro duplicado. Aqui basta nao prever
	// nada -- desconhecido nunca e inconsistente.
	if d := req.Plan.GetAttribute(ctx, path.Root("definition"), &def); d.HasError() {
		return
	}
	// Definition desconhecida no plan (vem de outro recurso): nada a prever.
	if def.IsUnknown() || def.IsNull() {
		return
	}

	nomes, declarado, ok := nomesDePlano(def.ValueString())
	if !ok {
		// Definition ilegivel: deixa desconhecido. O apply vai falhar na
		// validacao de qualquer forma, e desconhecido nunca e inconsistente.
		return
	}
	if !declarado {
		// A definition nao declara `plans`. O merge de topo preenche a chave
		// com o que esta no servidor, entao os planos ficam como estao -- e o
		// state e a previsao exata.
		resp.PlanValue = req.StateValue
		return
	}

	anteriores := req.StateValue.Elements()
	elems := map[string]attr.Value{}
	for _, nome := range nomes {
		id, achou := anteriores[nome]
		if achou && !id.IsNull() && !id.IsUnknown() {
			elems[nome] = id
			continue
		}
		elems[nome] = types.StringUnknown()
	}

	v, d := types.MapValue(types.StringType, elems)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.PlanValue = v
}

// nomesDePlano le os nomes de plano de uma definition.
//
// `declarado` diz se a chave `plans` esta presente, o que e diferente de estar
// presente e vazia: ausente significa "nao mexe nos planos do servidor",
// enquanto `"plans": []` significa "apague todos" (e o apply e recusado, a
// menos que allow_plan_deletion esteja ligado).
func nomesDePlano(definicao string) (nomes []string, declarado bool, ok bool) {
	var d map[string]any
	if err := json.Unmarshal([]byte(definicao), &d); err != nil {
		return nil, false, false
	}
	bruto, presente := d["plans"]
	if !presente || bruto == nil {
		return nil, false, true
	}
	planos, certo := bruto.([]any)
	if !certo {
		return nil, false, false
	}
	for _, p := range planos {
		pm, certo := p.(map[string]any)
		if !certo {
			return nil, false, false
		}
		nome, _ := pm["name"].(string)
		if nome == "" {
			// Plano sem nome: PareiaPlanos nao consegue parear, e prever
			// seria chute.
			return nil, false, false
		}
		nomes = append(nomes, nome)
	}
	return nomes, true, true
}
