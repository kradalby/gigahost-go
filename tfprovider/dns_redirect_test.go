package tfprovider

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// TestParallelRedirectWritesOnOneZone holds redirects to the rule records
// follow: the zone API fails overlapping record writes, and a redirect write
// mutates the same zone, so it takes the zone lock too. Real time, not
// synctest: a bubble's clock stalls while a goroutine waits on a sync.Mutex.
func TestParallelRedirectWritesOnOneZone(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	objType := h.resourceObjectType("gigahost_dns_redirect")

	var inFlight atomic.Int32

	h.api.Route(http.MethodPost, "/dns/zones/5100/redirect").
		RespondWith(func(_ *http.Request, _ int) (int, string) {
			defer inFlight.Add(-1)

			if inFlight.Add(1) > 1 {
				return http.StatusInternalServerError,
					`{"meta":{"status":500,"message":"Unknown server error"}}`
			}

			time.Sleep(50 * time.Millisecond)

			return http.StatusOK, `{"meta":{"status":200}}`
		})

	var wg sync.WaitGroup

	for i := range 2 {
		wg.Go(func() {
			config := mkObject(objType, map[string]tftypes.Value{
				"zone_id":    tfStr("5100"),
				"source":     tfStr(fmt.Sprintf("r%d", i)),
				"target_url": tfStr("https://example.no/"),
			})

			planned := h.plan("gigahost_dns_redirect", nullObject(objType), config)
			if planned.HasError() {
				t.Errorf("plan %d: %s", i, planned.ErrorText())

				return
			}

			res := h.apply("gigahost_dns_redirect", nullObject(objType), planned.plannedValue, config)
			if res.HasError() {
				t.Errorf("create %d: %s", i, res.ErrorText())
			}
		})
	}

	wg.Wait()
}
