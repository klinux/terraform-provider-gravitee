package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// objetoDoRecurso monta um valor com o esquema real do recurso, com tudo nulo
// menos a definition. O modifier le `definition` do plan, e o tipo precisa
// bater com o que o framework monta, senao GetAttribute falha.
func objetoDoRecurso(definicao string) tftypes.Value {
	tipo := schemaDeTeste().Type().TerraformType(context.Background()).(tftypes.Object)
	vals := map[string]tftypes.Value{}
	for nome, t := range tipo.AttributeTypes {
		vals[nome] = tftypes.NewValue(t, nil)
	}
	if definicao != "" {
		vals["definition"] = tftypes.NewValue(tftypes.String, definicao)
	}
	return tftypes.NewValue(tipo, vals)
}

func mapaDeIDs(m map[string]string) types.Map {
	elems := map[string]attr.Value{}
	for k, v := range m {
		elems[k] = types.StringValue(v)
	}
	out, d := types.MapValue(types.StringType, elems)
	if d.HasError() {
		panic(d)
	}
	return out
}

func defComPlanos(nomes ...string) string {
	s := `{"name":"x","plans":[`
	for i, n := range nomes {
		if i > 0 {
			s += ","
		}
		s += `{"name":"` + n + `","security":"KEY_LESS"}`
	}
	return s + `]}`
}

func TestPlanIDsPorNome(t *testing.T) {
	casos := []struct {
		nome        string
		criando     bool
		apagando    bool
		definicao   string
		noState     map[string]string
		stateNulo   bool
		jaConhecido bool

		querIntocado  bool              // modifier nao mexeu no que entrou
		querDoState   bool              // copiou o state inteiro
		querConhecido map[string]string // nomes com id vindo do state
		querUnknown   []string          // nomes que tem de ficar desconhecidos
	}{
		{
			nome:    "create nao mexe: todo id sai do apply",
			criando: true, definicao: defComPlanos("Default"),
			querIntocado: true,
		},
		{
			nome:     "destroy nao mexe",
			apagando: true, definicao: defComPlanos("Default"),
			querIntocado: true,
		},
		{
			nome:         "plan sem mudanca: valor ja conhecido fica como esta",
			definicao:    defComPlanos("Default"),
			noState:      map[string]string{"Default": "id-default"},
			jaConhecido:  true,
			querIntocado: true,
		},
		{
			// O caso que derrubou a partners em producao.
			nome:          "plano novo entra desconhecido, os antigos mantem o id",
			definicao:     defComPlanos("Default", "OAuth2"),
			noState:       map[string]string{"Default": "id-default"},
			querConhecido: map[string]string{"Default": "id-default"},
			querUnknown:   []string{"OAuth2"},
		},
		{
			nome:          "mesmos planos: mapa conhecido, sem ruido e sem replace de subscription",
			definicao:     defComPlanos("Default", "ApiKey"),
			noState:       map[string]string{"Default": "id-default", "ApiKey": "id-apikey"},
			querConhecido: map[string]string{"Default": "id-default", "ApiKey": "id-apikey"},
		},
		{
			nome:          "plano removido da definition sai do mapa planejado",
			definicao:     defComPlanos("Default"),
			noState:       map[string]string{"Default": "id-default", "ApiKey": "id-apikey"},
			querConhecido: map[string]string{"Default": "id-default"},
		},
		{
			nome:        "definition sem a chave plans: o merge de topo preserva o servidor, entao o state e exato",
			definicao:   `{"name":"x"}`,
			noState:     map[string]string{"Default": "id-default"},
			querDoState: true,
		},
		{
			nome:         "definition ilegivel fica desconhecido",
			definicao:    `{nao e json`,
			noState:      map[string]string{"Default": "id-default"},
			querIntocado: true,
		},
		{
			nome:         "plano sem nome fica desconhecido: parear seria chute",
			definicao:    `{"name":"x","plans":[{"security":"KEY_LESS"}]}`,
			noState:      map[string]string{"Default": "id-default"},
			querIntocado: true,
		},
		{
			nome:         "state nulo nao gera previsao",
			definicao:    defComPlanos("Default"),
			stateNulo:    true,
			querIntocado: true,
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			esq := schemaDeTeste()
			tipo := esq.Type().TerraformType(context.Background()).(tftypes.Object)

			estadoRaw := objetoDoRecurso(c.definicao)
			if c.criando {
				estadoRaw = tftypes.NewValue(tipo, nil)
			}
			planRaw := objetoDoRecurso(c.definicao)
			if c.apagando {
				planRaw = tftypes.NewValue(tipo, nil)
			}

			req := planmodifier.MapRequest{
				State: tfsdk.State{Raw: estadoRaw, Schema: esq},
				Plan:  tfsdk.Plan{Raw: planRaw, Schema: esq},
			}
			if c.stateNulo {
				req.StateValue = types.MapNull(types.StringType)
			} else {
				req.StateValue = mapaDeIDs(c.noState)
			}
			if c.jaConhecido {
				req.PlanValue = mapaDeIDs(c.noState)
			} else {
				req.PlanValue = types.MapUnknown(types.StringType)
			}
			entrou := req.PlanValue

			resp := &planmodifier.MapResponse{PlanValue: req.PlanValue}
			planIDsPorNome{}.PlanModifyMap(context.Background(), req, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("diagnostico inesperado: %v", resp.Diagnostics)
			}

			switch {
			case c.querIntocado:
				if !resp.PlanValue.Equal(entrou) {
					t.Fatalf("esperava o valor de entrada %v, veio %v", entrou, resp.PlanValue)
				}
			case c.querDoState:
				if !resp.PlanValue.Equal(req.StateValue) {
					t.Fatalf("esperava o state %v, veio %v", req.StateValue, resp.PlanValue)
				}
			default:
				if resp.PlanValue.IsUnknown() || resp.PlanValue.IsNull() {
					t.Fatalf("esperava mapa conhecido, veio %v", resp.PlanValue)
				}
				got := resp.PlanValue.Elements()
				if len(got) != len(c.querConhecido)+len(c.querUnknown) {
					t.Fatalf("esperava %d elementos, veio %v", len(c.querConhecido)+len(c.querUnknown), got)
				}
				for nome, id := range c.querConhecido {
					v, ok := got[nome]
					if !ok {
						t.Fatalf("faltou o plano %q em %v", nome, got)
					}
					s, ok := v.(types.String)
					if !ok || s.IsUnknown() || s.ValueString() != id {
						t.Fatalf("plano %q: esperava %q conhecido, veio %v", nome, id, v)
					}
				}
				for _, nome := range c.querUnknown {
					v, ok := got[nome]
					if !ok {
						t.Fatalf("faltou o plano %q em %v", nome, got)
					}
					if !v.IsUnknown() {
						t.Fatalf("plano %q: esperava desconhecido (o id sai do apply), veio %v", nome, v)
					}
				}
			}
		})
	}
}

func TestNomesDePlano(t *testing.T) {
	casos := []struct {
		nome      string
		json      string
		querNomes []string
		querDecl  bool
		querOK    bool
	}{
		{"dois planos", defComPlanos("Default", "OAuth2"), []string{"Default", "OAuth2"}, true, true},
		{"sem a chave plans", `{"name":"x"}`, nil, false, true},
		{"plans nulo", `{"name":"x","plans":null}`, nil, false, true},
		{"plans vazio e declarado, nao ausente", `{"name":"x","plans":[]}`, nil, true, true},
		{"plans do tipo errado", `{"name":"x","plans":{"a":1}}`, nil, false, false},
		{"json invalido", `{`, nil, false, false},
		{"plano sem nome", `{"plans":[{"security":"KEY_LESS"}]}`, nil, false, false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			nomes, decl, ok := nomesDePlano(c.json)
			if ok != c.querOK || decl != c.querDecl {
				t.Fatalf("esperava ok=%v declarado=%v, veio ok=%v declarado=%v", c.querOK, c.querDecl, ok, decl)
			}
			if len(nomes) != len(c.querNomes) {
				t.Fatalf("esperava %v, veio %v", c.querNomes, nomes)
			}
			for i, n := range c.querNomes {
				if nomes[i] != n {
					t.Fatalf("esperava %v, veio %v", c.querNomes, nomes)
				}
			}
		})
	}
}
