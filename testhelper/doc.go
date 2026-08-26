// Package testhelper provides an httptest-based mock server for
// exercising the gigahost API client end-to-end.
//
// Tests register expectations with [Server.Expect] describing the
// method, path, headers, query and JSON body that the client should
// send, plus the canned response. Unmatched requests and missed
// expectations both fail the test.
//
// The fake listens on httptest's in-memory network rather than a TCP
// port, so it is reachable only through [Server.Client] and usable from
// inside a [testing/synctest] bubble.
//
// Typical usage:
//
//	func TestSomething(t *testing.T) {
//	    srv := testhelper.NewServer(t)
//	    srv.Expect("GET", "/dns/zones").
//	        RespondFixtureJSON(t, "testdata/dns/list_zones.json")
//
//	    c, _ := gigahost.NewClient(
//	        gigahost.WithToken("test"),
//	        gigahost.WithBaseURL(srv.URL()),
//	        gigahost.WithHTTPClient(srv.Client()),
//	    )
//	    zones, err := c.DNS.ListZones(context.Background())
//	    // ... assertions ...
//	    srv.AssertDone(t)
//	}
package testhelper
