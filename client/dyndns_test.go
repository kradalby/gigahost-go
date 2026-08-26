package client_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/kradalby/gigahost-go/client"
	"github.com/kradalby/gigahost-go/testhelper"
)

func TestDynDNSUpdate(t *testing.T) {
	t.Parallel()

	srv := testhelper.NewServer(t)

	srv.Expect("GET", "/dns/dyndns").
		WithBasicAuth("user@example.com", "secret").
		WithQueryPairs("hostname", "home.example.no", "myip", "1.2.3.4").
		Respond(http.StatusOK, "good 1.2.3.4\n")

	c, err := client.NewClient(
		client.WithToken("t"),
		client.WithBaseURL(srv.URL()),
		client.WithHTTPClient(srv.Client()),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	results, err := c.DynDNS.Update(context.Background(), client.UpdateRequest{
		Username:  "user@example.com",
		Password:  "secret",
		Hostnames: []string{"home.example.no"},
		IPv4:      "1.2.3.4",
	})
	if err != nil {
		t.Fatalf("DynDNS.Update: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}

	if results[0].Response != client.DynDNSGood {
		t.Errorf("Response = %q, want %q", results[0].Response, client.DynDNSGood)
	}

	if results[0].IP != "1.2.3.4" {
		t.Errorf("IP = %q", results[0].IP)
	}

	if results[0].Hostname != "home.example.no" {
		t.Errorf("Hostname = %q", results[0].Hostname)
	}
}

func TestDynDNSUpdateMultipleHosts(t *testing.T) {
	t.Parallel()

	srv := testhelper.NewServer(t)

	srv.Expect("GET", "/dns/dyndns").
		WithQueryPairs("hostname", "a.example.no,b.example.no").
		Respond(http.StatusOK, "good 1.2.3.4\nnochg 1.2.3.4\n")

	c, err := client.NewClient(
		client.WithCredentials("u", "p", 0),
		client.WithBaseURL(srv.URL()),
		client.WithHTTPClient(srv.Client()),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	results, err := c.DynDNS.Update(context.Background(), client.UpdateRequest{
		Hostnames: []string{"a.example.no", "b.example.no"},
	})
	if err != nil {
		t.Fatalf("DynDNS.Update: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}

	if results[0].Response != client.DynDNSGood || results[1].Response != client.DynDNSNoChange {
		t.Errorf("results: %+v", results)
	}
}
