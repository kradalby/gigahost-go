package tfprovider

import "sync"

// zoneLocks serializes mutating operations against a single DNS zone.
//
// The gigahost DNS API returns 500 "Unable to delete record: Unknown server
// error" (and similar) when record create/update/delete calls for the same
// zone arrive concurrently — as they do during a parallel `terraform apply` or
// `destroy`. Serializing per-zone in the provider process avoids the race while
// still allowing parallelism across different zones. Redirect writes take it
// too: they mutate the same zone.
var zoneLocks sync.Map // zoneID -> *sync.Mutex

// lockZone acquires the lock for a zone and returns the unlock function:
//
//	defer lockZone(zoneID)()
func lockZone(zoneID string) func() {
	v, _ := zoneLocks.LoadOrStore(zoneID, &sync.Mutex{})

	mu, _ := v.(*sync.Mutex)
	mu.Lock()

	return mu.Unlock
}

// serverLocks serializes mutating operations against a single server.
//
// Ordering an additional IPv4 (gigahost_server_ipv4) and taking a snapshot
// (gigahost_server_snapshot) cannot learn the new object's id from the
// response, so Create identifies it by diffing the server's list before and
// after. Two concurrent creates on the same server would race that diff;
// serializing per-server keeps the before/after stable while still allowing
// parallelism across different servers.
var serverLocks sync.Map // serverID -> *sync.Mutex

// lockServer acquires the lock for a server and returns the unlock function:
//
//	defer lockServer(serverID)()
func lockServer(serverID string) func() {
	v, _ := serverLocks.LoadOrStore(serverID, &sync.Mutex{})

	mu, _ := v.(*sync.Mutex)
	mu.Lock()

	return mu.Unlock
}
