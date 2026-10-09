package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Platform flows apply to every API on the gateway, so this test only reads.
// It imports what is already configured and checks the round-trip reports no
// drift; it never writes. Even that is opt-in, because importing points the
// state at shared configuration.
//
// Set GRAVITEE_ACC_PLATFORM_FLOWS=1 to run it.
func TestAccPlatformFlows_importESemDrift(t *testing.T) {
	if os.Getenv("GRAVITEE_ACC_PLATFORM_FLOWS") == "" {
		t.Skip("set GRAVITEE_ACC_PLATFORM_FLOWS=1 to run; it reads gateway-wide configuration")
	}
	org := os.Getenv("GRAVITEE_ORGANIZATION")
	if org == "" {
		org = "DEFAULT"
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acPreCheck(t) },
		ProtoV6ProviderFactories: acProviders,
		Steps: []resource.TestStep{
			{
				// `flows` is Required, so the configuration has to carry a
				// value; ImportStateVerify then compares it against what the
				// server returns.
				Config:             `resource "gravitee_platform_flows" "p" { flows = "[]" }`,
				ResourceName:       "gravitee_platform_flows.p",
				ImportState:        true,
				ImportStateId:      org,
				ImportStatePersist: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gravitee_platform_flows.p", "id", org),
					resource.TestCheckResourceAttrSet("gravitee_platform_flows.p", "flow_mode"),
				),
			},
		},
	})
}
