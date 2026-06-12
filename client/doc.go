// Package client is a Go client for the Gigahost API.
//
// Gigahost is a Norwegian hosting provider offering virtual and dedicated
// servers, DNS zone hosting, .no domain registration, BGP peering and more.
// This package provides strongly typed access to their HTTP API, documented
// at https://gigahost.no/en/api-dokumentasjon.
//
// Import as:
//
//	import "github.com/kradalby/gigahost-go/client"
//
// # Getting started
//
// Create a client with either a bearer token or username/password
// credentials. When credentials are used, the client fetches and refreshes
// the token automatically:
//
//	c, err := client.NewClient(
//	    client.WithToken(os.Getenv("GIGAHOST_TOKEN")),
//	)
//	if err != nil { /* ... */ }
//
//	zones, err := c.DNS.ListZones(ctx)
//	for _, z := range zones {
//	    fmt.Println(z.Name, z.Active, z.UpdatedAt)
//	}
//
// All methods take a [context.Context] as the first argument and return
// either strongly typed result values or a [*APIError] carrying HTTP
// status and the API's own error message.
//
// # Authentication
//
// There are three authentication mechanisms:
//
//   - Bearer token (via [WithToken]), obtained from POST /authenticate or
//     externally.
//   - Username and password (via [WithCredentials]); the client obtains a
//     token on first request and refreshes it after a 401.
//   - HTTP Basic authentication for the /dns/dyndns endpoint (see
//     [DynDNSService]).
//
// # JSON handling
//
// The Gigahost API returns many values as strings (for example "0"/"1"
// booleans and unix timestamps as strings). This package normalises those
// values into idiomatic Go types via custom [json.Unmarshaler]
// implementations. The exported types use the [encoding/json/v2] semantics
// via github.com/go-json-experiment/json.
package client
