package provider

import (
	"context"
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// definicaoSemantica suprime o diff quando a definicao no state e a da config
// sao o mesmo JSON, diferindo so em espacos ou ordem de chaves.
//
// E necessario porque o state guarda a forma canonica compacta que o Read
// escreve, enquanto o arquivo em disco e formatado para leitura. Sem isto, toda
// API adotada por import mostraria um diff de espacos e pediria um apply que
// escreveria no servidor sem mudar nada -- 206 imports inuteis na adocao da
// frota.
type definicaoSemantica struct{}

func (definicaoSemantica) Description(_ context.Context) string {
	return "mantem o valor do state quando a definicao e o mesmo JSON, ignorando espacos e ordem de chaves"
}

func (d definicaoSemantica) MarkdownDescription(ctx context.Context) string {
	return d.Description(ctx)
}

func (definicaoSemantica) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if mesmoJSONTexto(req.StateValue.ValueString(), req.ConfigValue.ValueString()) {
		resp.PlanValue = req.StateValue
	}
}

func mesmoJSONTexto(a, b string) bool {
	var va, vb any
	if json.Unmarshal([]byte(a), &va) != nil || json.Unmarshal([]byte(b), &vb) != nil {
		return false
	}
	ja, err1 := json.Marshal(normaliza(va))
	jb, err2 := json.Marshal(normaliza(vb))
	if err1 != nil || err2 != nil {
		return false
	}
	return string(ja) == string(jb)
}
