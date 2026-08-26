// Package tfprovider implements the Gigahost provider for OpenTofu
// and Terraform, built on the terraform-plugin-framework.
//
// Both OpenTofu and Terraform speak the same plugin protocol, so the
// same provider package is used by both tools without modification.
// OpenTofu is the primary development target.
//
// This package contains all the provider, resource and data source
// definitions. It is imported and launched by the thin binary in the
// sibling terraform-provider-gigahost module (destined for its own
// GitHub repository and both the OpenTofu and Terraform registries).
//
// # Running the provider
//
//	import (
//	    "context"
//	    "github.com/hashicorp/terraform-plugin-framework/providerserver"
//	    "github.com/kradalby/gigahost-go/tfprovider"
//	)
//
//	func main() {
//	    providerserver.Serve(context.Background(), tfprovider.New("0.1.0"), providerserver.ServeOpts{
//	        Address: "registry.terraform.io/kradalby/gigahost",
//	    })
//	}
package tfprovider
