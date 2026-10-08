package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccApplication_lifecycle(t *testing.T) {
	nome := acPrefixo + "app"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acPreCheck(t) },
		ProtoV6ProviderFactories: acProviders,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "gravitee_application" "t" {
  name        = %q
  description = "v1"
  client_id   = %q
}`, nome, nome),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gravitee_application.t", "name", nome),
					resource.TestCheckResourceAttr("gravitee_application.t", "description", "v1"),
					resource.TestCheckResourceAttr("gravitee_application.t", "client_id", nome),
					resource.TestCheckResourceAttr("gravitee_application.t", "app_type", "web"),
					resource.TestCheckResourceAttrSet("gravitee_application.t", "id"),
					resource.TestCheckResourceAttr("gravitee_application.t", "status", "ACTIVE"),
				),
			},
			{
				// Updating only the description must not clear anything else.
				// A regression here means the read-modify-write in Update broke:
				// omitting a field in the PUT clears it on the server.
				Config: fmt.Sprintf(`
resource "gravitee_application" "t" {
  name        = %q
  description = "v2"
  client_id   = %q
}`, nome, nome),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gravitee_application.t", "description", "v2"),
					resource.TestCheckResourceAttr("gravitee_application.t", "client_id", nome),
					resource.TestCheckResourceAttrSet("gravitee_application.t", "groups.#"),
				),
			},
			{
				ResourceName:      "gravitee_application.t",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// The server assigns a group when none is declared, and an update must not
// take it away. This is the regression that motivated the merge semantics.
func TestAccApplication_grupoSobreviveAoUpdate(t *testing.T) {
	nome := acPrefixo + "grupo"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acPreCheck(t) },
		ProtoV6ProviderFactories: acProviders,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "gravitee_application" "g" {
  name        = %q
  description = "v1"
}`, nome),
				Check: resource.TestCheckResourceAttr("gravitee_application.g", "groups.#", "1"),
			},
			{
				Config: fmt.Sprintf(`
resource "gravitee_application" "g" {
  name        = %q
  description = "v2"
}`, nome),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gravitee_application.g", "description", "v2"),
					resource.TestCheckResourceAttr("gravitee_application.g", "groups.#", "1"),
				),
			},
		},
	})
}

func TestAccApplicationDataSource_porNome(t *testing.T) {
	nome := acPrefixo + "ds-app"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acPreCheck(t) },
		ProtoV6ProviderFactories: acProviders,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "gravitee_application" "d" {
  name        = %q
  description = "data source target"
  client_id   = %q
}

data "gravitee_application" "byname" {
  name       = gravitee_application.d.name
  depends_on = [gravitee_application.d]
}`, nome, nome),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.gravitee_application.byname", "id", "gravitee_application.d", "id"),
					resource.TestCheckResourceAttr("data.gravitee_application.byname", "client_id", nome),
				),
			},
		},
	})
}
