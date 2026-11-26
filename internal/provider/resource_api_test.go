package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// definicaoV2 builds a minimal v2 API definition. `extra` is spliced in as
// additional plans, so a step can add or drop one.
func definicaoV2(nome, path, conteudoMock string, comApiKey bool) string {
	planoApiKey := ""
	if comApiKey {
		planoApiKey = `,
    {
      "name": "ApiKey",
      "description": "key plan",
      "security": "API_KEY",
      "status": "PUBLISHED",
      "validation": "AUTO",
      "paths": {},
      "flows": []
    }`
	}
	return fmt.Sprintf(`{
  "name": %q,
  "version": "v1",
  "description": "acceptance test, safe to delete",
  "visibility": "PRIVATE",
  "gravitee": "2.0.0",
  "flow_mode": "BEST_MATCH",
  "proxy": {
    "virtual_hosts": [{ "path": %q }],
    "strip_context_path": false,
    "preserve_host": false,
    "groups": [
      {
        "name": "default-group",
        "endpoints": [
          { "name": "default", "target": "https://example.invalid", "type": "http",
            "weight": 1, "backup": false, "inherit": true }
        ],
        "load_balancing": { "type": "ROUND_ROBIN" }
      }
    ]
  },
  "flows": [
    {
      "name": "health",
      "path-operator": { "path": "/health", "operator": "STARTS_WITH" },
      "enabled": true, "methods": [], "condition": "", "consumers": [],
      "pre": [
        { "name": "Mock", "policy": "mock", "enabled": true,
          "configuration": { "status": "200", "content": %q, "headers": [] } }
      ],
      "post": []
    }
  ],
  "plans": [
    {
      "name": "Default",
      "description": "keyless",
      "security": "KEY_LESS",
      "status": "PUBLISHED",
      "validation": "AUTO",
      "paths": {},
      "flows": []
    }%s
  ]
}`, nome, path, conteudoMock, planoApiKey)
}

func TestAccAPI_lifecycle(t *testing.T) {
	nome := acPrefixo + "api"
	path := "/" + nome
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acPreCheck(t) },
		ProtoV6ProviderFactories: acProviders,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "gravitee_api" "t" {
  definition = <<-JSON
%s
  JSON
  deploy = false
}`, definicaoV2(nome, path, "v1", true)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gravitee_api.t", "name", nome),
					resource.TestCheckResourceAttr("gravitee_api.t", "context_path", path),
					resource.TestCheckResourceAttrSet("gravitee_api.t", "plan_ids.Default"),
					resource.TestCheckResourceAttrSet("gravitee_api.t", "plan_ids.ApiKey"),
				),
			},
			{
				// Changing a policy's configuration must not disturb plan ids:
				// the import pairs plans by id, and losing them would recreate
				// the plans and take their subscriptions down.
				Config: fmt.Sprintf(`
resource "gravitee_api" "t" {
  definition = <<-JSON
%s
  JSON
  deploy = false
}`, definicaoV2(nome, path, "v2", true)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("gravitee_api.t", "plan_ids.ApiKey"),
					resource.TestCheckResourceAttrSet("gravitee_api.t", "plan_ids.Default"),
				),
			},
			{
				// Dropping a plan from the definition must be refused by
				// default, because the import would delete it along with its
				// subscriptions.
				Config: fmt.Sprintf(`
resource "gravitee_api" "t" {
  definition = <<-JSON
%s
  JSON
  deploy = false
}`, definicaoV2(nome, path, "v2", false)),
				ExpectError: regexpPlanoApagado,
			},
		},
	})
}

// TestAccAPI_acrescentaPlano cobre o apply que ACRESCENTA um plano a uma API
// que ja existe. O lifecycle acima nasce com os dois planos, entao esse caminho
// nunca era exercitado -- e era onde o provider quebrava: com
// UseStateForUnknown em plan_ids o plan prometia o mapa do state e o servidor
// devolvia um nome a mais, o que o Terraform rejeita com "Provider produced
// inconsistent result after apply". Derrubou o job da partners em producao
// depois de a escrita e o deploy ja terem acontecido.
//
// Os ids tem de continuar estaveis no mesmo passo: se o id do plano que ja
// existia mudar, a subscription do parceiro vai embora.
func TestAccAPI_acrescentaPlano(t *testing.T) {
	nome := acPrefixo + "addplano"
	path := "/" + nome
	var idDefault string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acPreCheck(t) },
		ProtoV6ProviderFactories: acProviders,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "gravitee_api" "t" {
  definition = <<-JSON
%s
  JSON
  deploy = false
}`, definicaoV2(nome, path, "v1", false)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("gravitee_api.t", "plan_ids.Default"),
					resource.TestCheckNoResourceAttr("gravitee_api.t", "plan_ids.ApiKey"),
					guardaAtributo("gravitee_api.t", "plan_ids.Default", &idDefault),
				),
			},
			{
				Config: fmt.Sprintf(`
resource "gravitee_api" "t" {
  definition = <<-JSON
%s
  JSON
  deploy = false
}`, definicaoV2(nome, path, "v1", true)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("gravitee_api.t", "plan_ids.ApiKey"),
					confereAtributo("gravitee_api.t", "plan_ids.Default", &idDefault),
				),
			},
		},
	})
}

// guardaAtributo salva o valor de um atributo para um passo seguinte comparar.
func guardaAtributo(recurso, atributo string, destino *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[recurso]
		if !ok {
			return fmt.Errorf("recurso %s nao esta no state", recurso)
		}
		*destino = rs.Primary.Attributes[atributo]
		if *destino == "" {
			return fmt.Errorf("%s.%s esta vazio", recurso, atributo)
		}
		return nil
	}
}

// confereAtributo exige que o atributo continue com o valor guardado. Para
// plan_ids isso e o que separa "plano preservado" de "plano recriado, e a
// subscription do parceiro foi junto".
func confereAtributo(recurso, atributo string, esperado *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[recurso]
		if !ok {
			return fmt.Errorf("recurso %s nao esta no state", recurso)
		}
		if got := rs.Primary.Attributes[atributo]; got != *esperado {
			return fmt.Errorf("%s.%s mudou de %q para %q; o plano foi recriado e as subscriptions dele se perderam", recurso, atributo, *esperado, got)
		}
		return nil
	}
}

// A v1 definition must be refused during plan, not at apply.
func TestAccAPI_recusaV1(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acPreCheck(t) },
		ProtoV6ProviderFactories: acProviders,
		Steps: []resource.TestStep{
			{
				Config: `
resource "gravitee_api" "v1" {
  definition = jsonencode({
    name     = "tfacc-v1"
    gravitee = "1.0.0"
    paths    = {}
  })
}`,
				ExpectError: regexpV1,
				PlanOnly:    true,
			},
		},
	})
}

func TestAccAPIDataSource_porNome(t *testing.T) {
	nome := acPrefixo + "ds-api"
	path := "/" + nome
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acPreCheck(t) },
		ProtoV6ProviderFactories: acProviders,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "gravitee_api" "d" {
  definition = <<-JSON
%s
  JSON
  deploy = false
}

data "gravitee_api" "byname" {
  name       = gravitee_api.d.name
  depends_on = [gravitee_api.d]
}`, definicaoV2(nome, path, "v1", true)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.gravitee_api.byname", "id", "gravitee_api.d", "id"),
					resource.TestCheckResourceAttr("data.gravitee_api.byname", "context_path", path),
					resource.TestCheckResourceAttrSet("data.gravitee_api.byname", "plan_ids.ApiKey"),
				),
			},
		},
	})
}

// A subscription over a plan of an API created in the same run, with the plan
// id taken from plan_ids rather than hardcoded.
func TestAccSubscription_lifecycle(t *testing.T) {
	nome := acPrefixo + "sub"
	path := "/" + nome
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acPreCheck(t) },
		ProtoV6ProviderFactories: acProviders,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "gravitee_api" "s" {
  definition = <<-JSON
%s
  JSON
  deploy = false
}

resource "gravitee_application" "s" {
  name        = %q
  description = "acceptance test consumer"
}

resource "gravitee_subscription" "s" {
  application_id = gravitee_application.s.id
  plan_id        = gravitee_api.s.plan_ids["ApiKey"]
}`, definicaoV2(nome, path, "v1", true), nome),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gravitee_subscription.s", "status", "ACCEPTED"),
					resource.TestCheckResourceAttrPair("gravitee_subscription.s", "api_id", "gravitee_api.s", "id"),
					resource.TestCheckResourceAttrSet("gravitee_subscription.s", "id"),
				),
			},
		},
	})
}
