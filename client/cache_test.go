package client_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	client "github.com/kradalby/gigahost-go/client"
	"github.com/kradalby/gigahost-go/testhelper"
)

// TestCatalogIsFetchedOnce is the point of the cache. A Terraform refresh asks
// every resource independently, and each one resolves its size against the
// catalog: without this, fifty servers meant fifty identical downloads of the
// same 28KB payload.
func TestCatalogIsFetchedOnce(t *testing.T) {
	t.Parallel()

	srv := testhelper.NewServer(t)

	catalog := srv.Route(http.MethodGet, "/deploy/servers").
		RespondFixture(t, "testdata/deploy/catalog.json")

	c, err := client.NewClient(client.WithBaseURL(srv.URL()), client.WithHTTPClient(srv.Client()), client.WithToken("t"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx := context.Background()

	for range 5 {
		if _, err := c.Deploy.GetCatalog(ctx); err != nil {
			t.Fatalf("GetCatalog: %v", err)
		}
	}

	if got := catalog.Calls(); got != 1 {
		t.Errorf("catalog fetched %d times for 5 reads, want 1", got)
	}
}

// TestCatalogCacheIsConcurrencySafe covers the shape Terraform actually
// produces: ten resources refreshing at once, all wanting the catalog. They
// must make one request between them, not ten.
func TestCatalogCacheIsConcurrencySafe(t *testing.T) {
	t.Parallel()

	srv := testhelper.NewServer(t)

	catalog := srv.Route(http.MethodGet, "/deploy/servers").
		RespondFixture(t, "testdata/deploy/catalog.json")

	c, err := client.NewClient(client.WithBaseURL(srv.URL()), client.WithHTTPClient(srv.Client()), client.WithToken("t"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	var wg sync.WaitGroup

	for range 10 {
		wg.Go(func() {
			if _, err := c.Deploy.GetCatalog(context.Background()); err != nil {
				t.Errorf("GetCatalog: %v", err)
			}
		})
	}

	wg.Wait()

	if got := catalog.Calls(); got != 1 {
		t.Errorf("catalog fetched %d times from 10 concurrent readers, want 1", got)
	}
}

// TestOperatingSystemListIsFetchedOnce covers the more expensive one: building
// the list costs one request per distribution, so a single refresh made ten.
func TestOperatingSystemListIsFetchedOnce(t *testing.T) {
	t.Parallel()

	srv := testhelper.NewServer(t)

	distros := srv.Route(http.MethodGet, "/reinstall/distro").
		RespondFixture(t, "testdata/reinstall/distros.json")
	perDistro := srv.Route(http.MethodGet, "/reinstall/distro/*").
		RespondFixture(t, "testdata/reinstall/distro_debian.json")

	c, err := client.NewClient(client.WithBaseURL(srv.URL()), client.WithHTTPClient(srv.Client()), client.WithToken("t"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx := context.Background()

	first, err := c.Reinstall.ListAllOperatingSystems(ctx)
	if err != nil {
		t.Fatalf("ListAllOperatingSystems: %v", err)
	}

	perDistroFirst := perDistro.Calls()

	for range 4 {
		again, err := c.Reinstall.ListAllOperatingSystems(ctx)
		if err != nil {
			t.Fatalf("ListAllOperatingSystems: %v", err)
		}

		if len(again) != len(first) {
			t.Errorf("cached list has %d entries, first fetch had %d", len(again), len(first))
		}
	}

	if got := distros.Calls(); got != 1 {
		t.Errorf("distribution list fetched %d times for 5 reads, want 1", got)
	}

	if got := perDistro.Calls(); got != perDistroFirst {
		t.Errorf("per-distribution lists refetched: %d calls, want the first fetch's %d",
			got, perDistroFirst)
	}
}

// TestCacheDoesNotHideAnError pins that a failure is not memoised: the next
// caller must get a real attempt, not a cached failure or a stale success.
func TestCacheDoesNotHideAnError(t *testing.T) {
	t.Parallel()

	srv := testhelper.NewServer(t)

	srv.Route(http.MethodGet, "/deploy/servers").
		RespondWith(func(_ *http.Request, call int) (int, string) {
			if call == 1 {
				return http.StatusInternalServerError, `{"meta":{"status":500,"message":"blip"}}`
			}

			return http.StatusOK, `{"meta":{"status":200},"data":{"tiers":[],"regions":[],"currency":"NOK"}}`
		})

	c, err := client.NewClient(client.WithBaseURL(srv.URL()), client.WithHTTPClient(srv.Client()), client.WithToken("t"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx := context.Background()

	if _, err := c.Deploy.GetCatalog(ctx); err == nil {
		t.Fatal("first fetch should have failed")
	}

	if _, err := c.Deploy.GetCatalog(ctx); err != nil {
		t.Errorf("a failed fetch was memoised; the retry should have succeeded: %v", err)
	}
}

// TestCatalogRefetchesAfterTTL pins the other half of the cache contract: the
// entry expires. A long-lived client — the provider process during a big
// apply, or `gigahost` in a shell loop — must eventually see a catalog change
// rather than serving the first response forever.
//
// The synctest bubble makes the one-minute window free: the fake API is on an
// in-memory network, so advancing the bubble clock past referenceTTL costs no
// wall time.
func TestCatalogRefetchesAfterTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := testhelper.NewServer(t)

		catalog := srv.Route(http.MethodGet, "/deploy/servers").
			RespondFixture(t, "testdata/deploy/catalog.json")

		c, err := client.NewClient(
			client.WithBaseURL(srv.URL()),
			client.WithHTTPClient(srv.Client()),
			client.WithToken("t"),
		)
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}

		ctx := context.Background()

		if _, err := c.Deploy.GetCatalog(ctx); err != nil {
			t.Fatalf("GetCatalog: %v", err)
		}

		// One second short of the TTL the entry is still warm.
		synctest.Sleep(59 * time.Second)

		if _, err := c.Deploy.GetCatalog(ctx); err != nil {
			t.Fatalf("GetCatalog: %v", err)
		}

		if got := catalog.Calls(); got != 1 {
			t.Errorf("catalog fetched %d times inside the TTL, want 1", got)
		}

		// Crossing it forces exactly one refetch, not one per call.
		synctest.Sleep(2 * time.Second)

		for range 3 {
			if _, err := c.Deploy.GetCatalog(ctx); err != nil {
				t.Fatalf("GetCatalog: %v", err)
			}
		}

		if got := catalog.Calls(); got != 2 {
			t.Errorf("catalog fetched %d times across the TTL boundary, want 2", got)
		}
	})
}
