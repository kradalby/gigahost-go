package client

import (
	"sync"
	"time"
)

// referenceTTL is how long the catalog and the operating-system list are
// reused. They describe what Gigahost sells, not what the account owns, so
// they change on the order of weeks — but a client can be long-lived, so this
// is a cache with an expiry rather than a value fetched once.
//
// The window only has to span one Terraform plan or apply to remove almost
// every duplicate request.
const referenceTTL = time.Minute

// cached memoises one value for referenceTTL. It exists because a Terraform
// refresh asks each resource independently, and each one used to refetch the
// same reference data: a single server refresh made twelve requests, eleven of
// them for identical bytes, and fifty servers made six hundred.
//
// Deliberately not used for anything the account owns. Records, zones and
// servers change during an apply, and serving those from a cache would make
// create-then-look-up fail.
type cached[T any] struct {
	mu      sync.Mutex
	value   T
	expires time.Time
	ok      bool
}

// get returns the memoised value, calling fetch when it is absent or stale.
//
// The lock is held across fetch so a burst of parallel resources makes one
// request rather than all of them missing together. That serialises the first
// caller's peers for the length of one request, which is the point.
func (c *cached[T]) get(fetch func() (T, error)) (T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ok && time.Now().Before(c.expires) {
		return c.value, nil
	}

	value, err := fetch()
	if err != nil {
		var zero T

		return zero, err
	}

	c.value = value
	c.expires = time.Now().Add(referenceTTL)
	c.ok = true

	return value, nil
}
