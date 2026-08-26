package tfprovider

import (
	"sync"
	"sync/atomic"
	"testing"
)

// The zone lock exists because the API returns 500 when two record writes hit
// the same zone at once (upstream B8). Terraform runs resources in parallel by
// default, so a zone with several records reaches that path on every apply —
// and none of it was tested.

// TestLockZoneSerialisesSameZone proves the lock actually excludes. Without
// it, concurrent record writes to one zone race and the API rejects them.
func TestLockZoneSerialisesSameZone(t *testing.T) {
	t.Parallel()

	const goroutines = 16

	var (
		inside  atomic.Int32
		maxSeen atomic.Int32
		wg      sync.WaitGroup
	)

	for range goroutines {
		wg.Go(func() {
			unlock := lockZone("zone-serialise")
			defer unlock()

			n := inside.Add(1)

			for {
				peak := maxSeen.Load()
				if n <= peak || maxSeen.CompareAndSwap(peak, n) {
					break
				}
			}

			inside.Add(-1)
		})
	}

	wg.Wait()

	if got := maxSeen.Load(); got != 1 {
		t.Errorf("%d goroutines were inside the zone lock at once, want 1; "+
			"concurrent writes to one zone are rejected by the API (upstream B8)", got)
	}
}

// TestLockZoneAllowsDifferentZones is the other half: the lock must be per
// zone, or an apply touching many zones serialises entirely and a large
// configuration crawls.
func TestLockZoneAllowsDifferentZones(t *testing.T) {
	t.Parallel()

	first := lockZone("zone-a")
	defer first()

	// A different zone must not block. If it does, this send never happens and
	// the test fails on the closed channel rather than hanging forever.
	done := make(chan struct{})

	go func() {
		unlock := lockZone("zone-b")
		defer unlock()

		close(done)
	}()

	<-done
}

// TestLockServerSerialisesSameServer covers the sibling lock, which guards the
// IPv4 order path: ordering two addresses at once makes the before/after diff
// used to discover the new address ambiguous.
func TestLockServerSerialisesSameServer(t *testing.T) {
	t.Parallel()

	var (
		inside  atomic.Int32
		maxSeen atomic.Int32
		wg      sync.WaitGroup
	)

	for range 8 {
		wg.Go(func() {
			unlock := lockServer("srv-1")
			defer unlock()

			n := inside.Add(1)

			for {
				peak := maxSeen.Load()
				if n <= peak || maxSeen.CompareAndSwap(peak, n) {
					break
				}
			}

			inside.Add(-1)
		})
	}

	wg.Wait()

	if got := maxSeen.Load(); got != 1 {
		t.Errorf("%d goroutines held the server lock at once, want 1", got)
	}
}
