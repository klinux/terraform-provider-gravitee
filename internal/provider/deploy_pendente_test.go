package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Esquema minimo com os dois atributos que o modifier le. O tipo precisa bater
// com o que o framework monta, senao GetAttribute falha.
func esquemaSync() tftypes.Object {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"deploy":       tftypes.Bool,
		"synchronized": tftypes.Bool,
	}}
}

func valorSync(deploy, sync *bool) tftypes.Value {
	v := func(b *bool) tftypes.Value {
		if b == nil {
			return tftypes.NewValue(tftypes.Bool, nil)
		}
		return tftypes.NewValue(tftypes.Bool, *b)
	}
	return tftypes.NewValue(esquemaSync(), map[string]tftypes.Value{
		"deploy":       v(deploy),
		"synchronized": v(sync),
	})
}

func TestDeployPendente(t *testing.T) {
	sim, nao := true, false

	casos := []struct {
		nome        string
		criando     bool
		deploy      bool
		noState     *bool
		querUnknown bool
		querValor   *bool
	}{
		{
			nome:    "create nao mexe: o valor sai do apply",
			criando: true, deploy: true,
		},
		{
			nome:   "dessincronizada com deploy ligado vira desconhecido, gerando diff",
			deploy: true, noState: &nao, querUnknown: true,
		},
		{
			nome:   "sincronizada preserva o state, sem ruido no plan",
			deploy: true, noState: &sim, querValor: &sim,
		},
		{
			nome:   "dessincronizada com deploy desligado nao gera diff",
			deploy: false, noState: &nao, querValor: &nao,
		},
		{
			nome:   "valor nulo no state nao gera diff",
			deploy: true, noState: nil, querValor: nil,
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			esq := esquemaSync()
			estado := tfsdk.State{Raw: valorSync(&c.deploy, c.noState), Schema: schemaDeTeste()}
			if c.criando {
				estado.Raw = tftypes.NewValue(esq, nil)
			}
			req := planmodifier.BoolRequest{
				State: estado,
				Plan:  tfsdk.Plan{Raw: valorSync(&c.deploy, c.noState), Schema: schemaDeTeste()},
			}
			if c.noState == nil {
				req.StateValue = types.BoolNull()
			} else {
				req.StateValue = types.BoolValue(*c.noState)
			}
			resp := &planmodifier.BoolResponse{PlanValue: req.StateValue}

			deployPendente{}.PlanModifyBool(context.Background(), req, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("diagnostico inesperado: %v", resp.Diagnostics)
			}
			switch {
			case c.criando:
				// nao mexeu: segue o que entrou
			case c.querUnknown:
				if !resp.PlanValue.IsUnknown() {
					t.Fatalf("esperava desconhecido (para gerar diff), veio %v", resp.PlanValue)
				}
			case c.querValor == nil:
				if !resp.PlanValue.IsNull() {
					t.Fatalf("esperava nulo, veio %v", resp.PlanValue)
				}
			default:
				if resp.PlanValue.IsUnknown() || resp.PlanValue.ValueBool() != *c.querValor {
					t.Fatalf("esperava %v, veio %v", *c.querValor, resp.PlanValue)
				}
			}
		})
	}
}

// schemaDeTeste devolve o esquema real do recurso, para o GetAttribute do
// modifier encontrar o atributo "deploy".
func schemaDeTeste() schema.Schema {
	var resp resource.SchemaResponse
	(&apiResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	return resp.Schema
}

var _ = attr.Value(types.BoolNull())
