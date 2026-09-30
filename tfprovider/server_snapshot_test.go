package tfprovider

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// serverSnapshots stands in for one server's snapshot list. The API's create
// returns no ID, so what the provider adopts depends entirely on what the list
// shows afterwards; a store lets each test decide that.
type serverSnapshots struct {
	mu    sync.Mutex
	snaps []string // display names; a snapshot's id is its index plus one
}

func (s *serverSnapshots) add(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.snaps = append(s.snaps, name)
}

func (s *serverSnapshots) listJSON(serverID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	var b strings.Builder

	b.WriteString(`{"meta":{"status":200},"data":[`)

	for i, name := range s.snaps {
		if i > 0 {
			b.WriteString(",")
		}

		fmt.Fprintf(&b, `{"snap_id":%d,"srv_id":%s,"snap_name":"rnd%d","snap_display_name":%q,"snap_time":1700000000,"snap_state":"completed"}`,
			i+1, serverID, i+1, name)
	}

	b.WriteString(`]}`)

	return b.String()
}

// wireSnapshotRoutes serves the store; onCreate decides what a create adds.
func wireSnapshotRoutes(h *harness, serverID string, s *serverSnapshots, onCreate func(name string)) {
	h.api.Route(http.MethodGet, "/servers/"+serverID+"/snapshots").
		RespondWith(func(_ *http.Request, _ int) (int, string) {
			return http.StatusOK, s.listJSON(serverID)
		})

	h.api.Route(http.MethodPost, "/servers/"+serverID+"/snapshot").
		RespondWith(func(r *http.Request, _ int) (int, string) {
			var body struct {
				Name string `json:"name"`
			}

			if err := json.UnmarshalRead(r.Body, &body); err != nil {
				return http.StatusBadRequest, `{"meta":{"status":400}}`
			}

			onCreate(body.Name)

			return http.StatusOK, `{"meta":{"status":200}}`
		})
}

func createSnapshot(h *harness, serverID, name string) applyResult {
	h.t.Helper()

	objType := h.resourceObjectType("gigahost_server_snapshot")
	config := mkObject(objType, map[string]tftypes.Value{
		"server_id": tfStr(serverID), "name": tfStr(name),
	})

	planned := h.plan("gigahost_server_snapshot", nullObject(objType), config)
	if planned.HasError() {
		h.t.Fatalf("plan: %s", planned.ErrorText())
	}

	return h.apply("gigahost_server_snapshot", nullObject(objType), planned.plannedValue, config)
}

// TestSnapshotCreateAdoptsTheNewSnapshot guards against adopting a namesake.
// A snapshot of the same name already exists — taken by hand, or the old half
// of create_before_destroy. Adopting it puts the wrong ID in state, and the
// next destroy deletes the user's snapshot.
func TestSnapshotCreateAdoptsTheNewSnapshot(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)

		snaps := &serverSnapshots{}
		snaps.add("nightly")
		wireSnapshotRoutes(h, "18401", snaps, snaps.add)

		res := createSnapshot(h, "18401", "nightly")
		if res.HasError() {
			t.Fatalf("create: %s", res.ErrorText())
		}

		if got := str(res.State, "snapshot_id"); got != "2" {
			t.Errorf("snapshot_id = %s, want 2: the existing namesake was adopted", got)
		}
	})
}

// TestSnapshotCreateRefusesToGuess covers two new snapshots of the requested
// name appearing at once, one of them made outside this provider. Either could
// be ours; picking one risks a later destroy deleting the other.
func TestSnapshotCreateRefusesToGuess(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)

		snaps := &serverSnapshots{}
		wireSnapshotRoutes(h, "18402", snaps, func(name string) {
			snaps.add(name)
			snaps.add(name)
		})

		res := createSnapshot(h, "18402", "nightly")
		if !res.HasError() {
			t.Fatalf("adopted snapshot %s out of two candidates", str(res.State, "snapshot_id"))
		}

		if !strings.Contains(res.ErrorText(), "ambiguous") {
			t.Errorf("error %q does not say the match was ambiguous", res.ErrorText())
		}
	})
}

// TestParallelSnapshotCreatesOnOneServer is the Terraform shape: two snapshot
// resources with the same name on one server, applied in parallel. Each must
// adopt its own snapshot, not fail on seeing the other's.
//
// Real time rather than a synctest bubble: the per-server lock is a
// sync.Mutex, which a bubble does not count as durably blocked, so its clock
// would never advance past the slow create.
func TestParallelSnapshotCreatesOnOneServer(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	// A slow create is the window in which the other resource reads its
	// before-list.
	snaps := &serverSnapshots{}
	wireSnapshotRoutes(h, "18403", snaps, func(name string) {
		time.Sleep(100 * time.Millisecond)
		snaps.add(name)
	})

	var (
		wg  sync.WaitGroup
		ids [2]string
	)

	for i := range ids {
		wg.Go(func() {
			res := createSnapshot(h, "18403", "nightly")
			if res.HasError() {
				t.Errorf("create %d: %s", i, res.ErrorText())

				return
			}

			ids[i] = str(res.State, "snapshot_id")
		})
	}

	wg.Wait()

	if ids[0] == ids[1] {
		t.Errorf("both resources adopted snapshot %q", ids[0])
	}
}
