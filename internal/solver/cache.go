package solver

import (
	"context"
	"sync"
	"time"

	"github.com/hayone1/cert-manager-webhook-contabo/internal/contabo"
)

const (
	// clientIdleTTL drops a cached client that was not used for this long,
	// for example after a credential rotation.
	clientIdleTTL = time.Hour
	// maxClients caps the cache when many issuers use different accounts.
	maxClients = 64
	// zoneCacheTTL is how long the zone list of an account is reused.
	// A miss always refreshes it, so a new zone is found at once.
	zoneCacheTTL = time.Minute
)

// clientCache keeps one Contabo client per set of credentials, so the
// access token, rate limit and zone list are shared by every challenge
// that uses the same account. Several issuers may use different accounts.
//
// The key is the four credentials themselves (kept only in memory, like the
// client that uses them), so a rotated password gets a fresh client and token
// at once. They are not hashed: a fast hash of a password is still a weak
// password hash, and a map key needs no hashing.
type clientCache struct {
	mu      sync.Mutex
	entries map[contabo.Credentials]*account
	newFn   func(contabo.Credentials) (*contabo.Client, error)
	now     func() time.Time
}

// account is one cached client plus its zone list.
type account struct {
	client *contabo.Client

	mu       sync.Mutex
	lastUsed time.Time
	zones    []string          // normalized names
	apiNames map[string]string // normalized name -> name as the API spells it
	zonesAt  time.Time
}

func newClientCache(newFn func(contabo.Credentials) (*contabo.Client, error), now func() time.Time) *clientCache {
	return &clientCache{entries: map[contabo.Credentials]*account{}, newFn: newFn, now: now}
}

func (cc *clientCache) get(creds contabo.Credentials) (*account, error) {
	key := creds
	now := cc.now()

	cc.mu.Lock()
	defer cc.mu.Unlock()
	for k, e := range cc.entries {
		e.mu.Lock()
		idle := now.Sub(e.lastUsed) > clientIdleTTL
		e.mu.Unlock()
		if idle {
			delete(cc.entries, k)
		}
	}
	if e, ok := cc.entries[key]; ok {
		e.touch(now)
		return e, nil
	}
	if len(cc.entries) >= maxClients {
		cc.evictOldest()
	}
	c, err := cc.newFn(creds)
	if err != nil {
		return nil, err
	}
	e := &account{client: c, lastUsed: now}
	cc.entries[key] = e
	return e, nil
}

func (cc *clientCache) evictOldest() {
	var oldestKey contabo.Credentials
	var oldest time.Time
	first := true
	for k, e := range cc.entries {
		e.mu.Lock()
		t := e.lastUsed
		e.mu.Unlock()
		if first || t.Before(oldest) {
			oldestKey, oldest, first = k, t, false
		}
	}
	delete(cc.entries, oldestKey)
}

func (a *account) touch(now time.Time) {
	a.mu.Lock()
	a.lastUsed = now
	a.mu.Unlock()
}

// listZones returns the normalized zone names of the account. It reuses a
// recent list unless refresh is set.
func (a *account) listZones(ctx context.Context, now time.Time, refresh bool) ([]string, error) {
	a.mu.Lock()
	if !refresh && a.zones != nil && now.Sub(a.zonesAt) < zoneCacheTTL {
		z := a.zones
		a.mu.Unlock()
		return z, nil
	}
	a.mu.Unlock()

	raw, err := a.client.ListZones(ctx)
	if err != nil {
		return nil, err
	}
	zones := make([]string, 0, len(raw))
	names := make(map[string]string, len(raw))
	for _, z := range raw {
		n, err := normalizeName(z.Name)
		if err != nil || n == "" {
			continue
		}
		if _, dup := names[n]; !dup {
			zones = append(zones, n)
		}
		names[n] = z.Name
	}

	a.mu.Lock()
	a.zones, a.apiNames, a.zonesAt = zones, names, now
	a.mu.Unlock()
	return zones, nil
}

// apiName returns the zone name as the API spells it, for use in URLs.
func (a *account) apiName(zone string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if n, ok := a.apiNames[zone]; ok {
		return n
	}
	return zone
}

// forgetZones drops the cached zone list, for example after the API said a
// zone does not exist.
func (a *account) forgetZones() {
	a.mu.Lock()
	a.zones = nil
	a.mu.Unlock()
}
