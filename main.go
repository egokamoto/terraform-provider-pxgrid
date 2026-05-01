package main

import (
	"context"
	"log"

	"github.com/bastet-cat/terraform-provider-pxgrid/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

func main() {
	if err := providerserver.Serve(context.Background(), provider.New, providerserver.ServeOpts{
		Address: "registry.terraform.io/bastet-cat/pxgrid",
	}); err != nil {
		log.Fatalf("error starting provider: %s", err)
	}
}
