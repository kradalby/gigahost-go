package client_test

import (
	"testing"

	"github.com/kradalby/gigahost-go/client"
	"github.com/kradalby/gigahost-go/testhelper"
)

// newServerAndClient builds a httptest server + client pair for
// use in service lifecycle tests. It is the shortest form; tests that
// need credential-based auth should construct manually.
func newServerAndClient(t *testing.T) (*testhelper.Server, *client.Client) {
	t.Helper()

	srv := testhelper.NewServer(t)
	c := newTestClient(t, srv)

	return srv, c
}
