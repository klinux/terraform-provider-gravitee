package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// Acceptance tests talk to a real Gravitee APIM. They only run with TF_ACC set,
// and need GRAVITEE_ENDPOINT and GRAVITEE_TOKEN pointing at an instance you are
// willing to have objects created and deleted in.
//
// Everything they create is named with the acPrefixo prefix and destroyed at
// the end of each step by the test framework.
const acPrefixo = "tfacc-"

var acProviders = map[string]func() (tfprotov6.ProviderServer, error){
	"gravitee": providerserver.NewProtocol6WithError(New("test")()),
}

func acPreCheck(t *testing.T) {
	t.Helper()
	for _, v := range []string{"GRAVITEE_ENDPOINT", "GRAVITEE_TOKEN"} {
		if os.Getenv(v) == "" {
			t.Fatalf("%s must be set for acceptance tests", v)
		}
	}
}

// acGrupo is optional: when set, applications are created in that group, which
// also exercises the group round-trip.
func acGrupo() string { return os.Getenv("GRAVITEE_ACC_GROUP") }
