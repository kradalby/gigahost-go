// Package e2e holds live end-to-end tests for the gigahost client, exercised
// against the real API using the GIGAHOST_TOKEN from the environment.
//
// Every file is constrained by the `e2e` build tag, so a plain `go test ./...`
// never compiles or runs them and thus never provisions billable resources.
// Run them explicitly with `go test -tags e2e ./e2e/...`.
package e2e
