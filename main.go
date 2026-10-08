package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/klinux/terraform-provider-gravitee/internal/provider"
)

// versao e sobrescrita no build: -ldflags "-X main.versao=1.2.3"
var versao = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "roda o provider em modo debug, para anexar um depurador")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(versao), providerserver.ServeOpts{
		Address: "registry.terraform.io/klinux/gravitee",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
