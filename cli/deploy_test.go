package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	gigahost "github.com/kradalby/gigahost-go/client"
	"github.com/kradalby/gigahost-go/testhelper"
)

// Two orders mid-provisioning, then the same two ready. Only the ready
// payload carries srv_id/ip/password, which is what the final table prints.
const (
	statusInstallingBody = `{
		"meta":{"status":200,"status_message":"200 OK"},
		"data":{
			"servers":[
				{"order_id":"100","order_number":"GH-100001","hostname":"web01.example.no","status":"installing"},
				{"order_id":"101","order_number":"GH-100002","hostname":"web02.example.no","status":"installing"}
			],
			"all_ready":false
		}
	}`

	statusReadyBody = `{
		"meta":{"status":200,"status_message":"200 OK"},
		"data":{
			"servers":[
				{"order_id":"100","order_number":"GH-100001","hostname":"web01.example.no","srv_id":"3600","ip":"185.181.63.100","status":"ready","password":"s3cr3t"},
				{"order_id":"101","order_number":"GH-100002","hostname":"web02.example.no","srv_id":"3601","ip":"185.181.63.101","status":"ready","password":"h0rsebat"}
			],
			"all_ready":true
		}
	}`
)

// TestPollDeployStatusWaitsForAllReady is the first test of `deploy create
// --wait`'s poll loop. Its 5s interval is a constant with no injection seam,
// so before synctest any test of it cost real seconds and nobody wrote one.
//
// Inside a bubble the waits are free, which makes the interval itself worth
// asserting: reaching a ready answer on the third response must cost exactly
// three intervals, and exactly three requests. Both were unobservable before.
func TestPollDeployStatusWaitsForAllReady(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		srv := testhelper.NewServer(t)

		status := srv.Route(http.MethodGet, "/deploy/status").
			RespondWith(func(r *http.Request, call int) (int, string) {
				// The loop must keep asking about every order it was given.
				if got, want := r.URL.Query().Get("ids"), "100,101"; got != want {
					t.Errorf("poll %d: ids query = %q, want %q", call, got, want)
				}

				if call < 3 {
					return http.StatusOK, statusInstallingBody
				}

				return http.StatusOK, statusReadyBody
			})

		cl, err := gigahost.NewClient(
			gigahost.WithBaseURL(srv.URL()),
			gigahost.WithHTTPClient(srv.Client()),
			gigahost.WithToken("t"),
		)
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}

		var out bytes.Buffer

		c := &Context{Out: &out, Err: &out, format: outputTable}

		start := time.Now()

		if err := pollDeployStatus(t.Context(), cl, c, []string{"100", "101"}); err != nil {
			t.Fatalf("pollDeployStatus: %v", err)
		}

		if got, want := time.Since(start), 15*time.Second; got != want {
			t.Errorf("elapsed = %v, want %v (3 polls at 5s)", got, want)
		}

		// One request per interval: no busy loop, and no early return while a
		// server was still installing.
		if got := status.Calls(); got != 3 {
			t.Errorf("status polled %d times, want 3", got)
		}

		// Two servers reported per non-final poll, so progress is printed as
		// it arrives rather than buffered until the end.
		if got := strings.Count(out.String(), "installing"); got != 4 {
			t.Errorf("printed %d installing lines, want 4", got)
		}

		// The ready table is the payload of the whole command: without it the
		// user never learns the server id, address or root password.
		for _, want := range []string{"3600", "185.181.63.100", "s3cr3t", "3601", "185.181.63.101", "h0rsebat"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("ready output missing %q\n%s", want, out.String())
			}
		}
	})
}

// TestPollDeployStatusHonoursContextCancellation pins the other exit from the
// loop. A user hitting Ctrl-C during `--wait` must abort inside the interval,
// not after issuing one more request against the API.
func TestPollDeployStatusHonoursContextCancellation(t *testing.T) {
	t.Parallel()

	srv := testhelper.NewServer(t)

	status := srv.Route(http.MethodGet, "/deploy/status").
		Respond(http.StatusOK, statusReadyBody)

	cl, err := gigahost.NewClient(
		gigahost.WithBaseURL(srv.URL()),
		gigahost.WithHTTPClient(srv.Client()),
		gigahost.WithToken("t"),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var out bytes.Buffer

	c := &Context{Out: &out, Err: &out, format: outputTable}

	if err := pollDeployStatus(ctx, cl, c, []string{"100"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pollDeployStatus err = %v, want context.Canceled", err)
	}

	if got := status.Calls(); got != 0 {
		t.Errorf("polled %d times after cancellation, want 0", got)
	}
}
