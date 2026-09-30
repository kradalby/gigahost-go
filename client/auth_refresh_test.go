package client_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	client "github.com/kradalby/gigahost-go/client"
	"github.com/kradalby/gigahost-go/testhelper"
)

// The Terraform provider drives one credentials-backed client at parallelism
// ten. Every one of those callers needs a token at the same moment — on the
// first request, and again when the token expires — so how the client shares
// a refresh decides whether an apply makes one login or ten.

// sessionAPI mints a new token per /authenticate and accepts only tokens it
// still considers valid. With singleSession set, each login revokes every
// earlier token: if Gigahost keeps one session per user, that is what parallel
// logins would do to each other.
type sessionAPI struct {
	singleSession bool

	mu     sync.Mutex
	minted int
	valid  map[string]bool
}

func newSessionAPI(t *testing.T, singleSession bool) (*sessionAPI, *client.Client) {
	t.Helper()

	api := &sessionAPI{singleSession: singleSession, valid: map[string]bool{}}
	srv := testhelper.NewServer(t)

	srv.Route(http.MethodPost, "/authenticate").RespondWith(api.authenticate)
	srv.Route(http.MethodGet, "/dns/zones").RespondWith(api.zones)

	return api, credentialsClient(t, srv)
}

func (a *sessionAPI) authenticate(_ *http.Request, _ int) (int, string) {
	// Latency is what lets parallel callers pile up behind one login.
	time.Sleep(20 * time.Millisecond)

	a.mu.Lock()
	defer a.mu.Unlock()

	a.minted++
	tok := fmt.Sprintf("tok-%d", a.minted)

	if a.singleSession {
		clear(a.valid)
	}

	a.valid[tok] = true

	return http.StatusOK, fmt.Sprintf(`{"meta":{"status":200},"data":{"token":%q}}`, tok)
}

// zones answers slower on each call, so some 401s for an expired token land
// only after another caller has already replaced it.
func (a *sessionAPI) zones(r *http.Request, call int) (int, string) {
	time.Sleep(time.Duration(call) * 10 * time.Millisecond)

	a.mu.Lock()
	ok := a.valid[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	a.mu.Unlock()

	if !ok {
		return http.StatusUnauthorized, `{"meta":{"status":401,"message":"Token expired."}}`
	}

	return http.StatusOK, `{"meta":{"status":200},"data":[]}`
}

func (a *sessionAPI) expireAll() {
	a.mu.Lock()
	defer a.mu.Unlock()

	clear(a.valid)
}

func (a *sessionAPI) logins() int {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.minted
}

// listZonesInParallel runs n concurrent calls and counts the failures.
func listZonesInParallel(c *client.Client, n int) int {
	var (
		wg     sync.WaitGroup
		failed atomic.Int32
	)

	for range n {
		wg.Go(func() {
			if _, err := c.DNS.ListZones(context.Background()); err != nil {
				failed.Add(1)
			}
		})
	}

	wg.Wait()

	return int(failed.Load())
}

// TestParallelCallersShareOneLogin pins one /authenticate per token lifetime,
// however many callers need it at once. On a single-session API, every extra
// login would revoke the token another caller had just been handed.
func TestParallelCallersShareOneLogin(t *testing.T) {
	t.Parallel()

	for _, singleSession := range []bool{false, true} {
		t.Run(fmt.Sprintf("cold/singleSession=%v", singleSession), func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				api, c := newSessionAPI(t, singleSession)

				if failed := listZonesInParallel(c, 10); failed != 0 {
					t.Errorf("%d of 10 parallel calls failed", failed)
				}

				if got := api.logins(); got != 1 {
					t.Errorf("%d logins for 10 parallel first calls, want 1", got)
				}
			})
		})

		t.Run(fmt.Sprintf("expiry/singleSession=%v", singleSession), func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				api, c := newSessionAPI(t, singleSession)

				if _, err := c.DNS.ListZones(context.Background()); err != nil {
					t.Fatalf("warm-up ListZones: %v", err)
				}

				api.expireAll()

				if failed := listZonesInParallel(c, 10); failed != 0 {
					t.Errorf("%d of 10 parallel calls failed after the token expired", failed)
				}

				if got := api.logins(); got != 2 {
					t.Errorf("%d logins in total, want 2: the warm-up and one shared refresh", got)
				}
			})
		})
	}
}

// TestRefreshWaiterHonoursItsContext keeps a shared login from pinning callers
// that have given up: a waiter whose context ends must return, not sit out a
// slow /authenticate that belongs to someone else.
func TestRefreshWaiterHonoursItsContext(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		srv := testhelper.NewServer(t)
		srv.Route(http.MethodPost, "/authenticate").RespondWith(func(_ *http.Request, _ int) (int, string) {
			time.Sleep(time.Hour)

			return http.StatusOK, authOKJSON
		})
		srv.Route(http.MethodGet, "/dns/zones").Respond(http.StatusOK, `{"meta":{"status":200},"data":[]}`)

		c := credentialsClient(t, srv)

		// The leader starts the login and blocks in it.
		leaderDone := make(chan struct{})

		go func() {
			defer close(leaderDone)

			_, _ = c.DNS.ListZones(context.Background())
		}()

		synctest.Wait()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		start := time.Now()

		_, err := c.DNS.ListZones(ctx)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("waiter error = %v, want its own deadline", err)
		}

		if waited := time.Since(start); waited != time.Second {
			t.Errorf("waiter returned after %s, want its 1s deadline", waited)
		}

		<-leaderDone
	})
}

// TestAbandonedRefreshIsRetried covers the leader giving up: its cancelled
// login says nothing about the credentials, so a waiter still willing to wait
// must log in itself rather than inherit someone else's cancellation.
func TestAbandonedRefreshIsRetried(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		_, c := newSessionAPI(t, false)

		leaderCtx, cancelLeader := context.WithCancel(context.Background())

		leaderErr := make(chan error, 1)

		go func() {
			_, err := c.DNS.ListZones(leaderCtx)
			leaderErr <- err
		}()

		synctest.Wait()

		waiterErr := make(chan error, 1)

		go func() {
			_, err := c.DNS.ListZones(context.Background())
			waiterErr <- err
		}()

		synctest.Wait()
		cancelLeader()

		if err := <-leaderErr; !errors.Is(err, context.Canceled) {
			t.Errorf("leader error = %v, want context.Canceled", err)
		}

		if err := <-waiterErr; err != nil {
			t.Errorf("waiter inherited the leader's cancellation: %v", err)
		}
	})
}
