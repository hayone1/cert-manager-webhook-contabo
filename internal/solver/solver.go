// Package solver is the cert-manager DNS-01 webhook solver for Contabo DNS.
//
// Present adds the _acme-challenge TXT record through the Contabo API and
// CleanUp removes it again. Both are safe to call many times, and both only
// ever touch the one TXT record whose value equals the challenge key, so
// several challenges for the same name (for example example.com and
// *.example.com in one certificate) can run at the same time.
package solver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook"
	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/hayone1/cert-manager-webhook-contabo/internal/contabo"
)

// Name is the solverName to use in the issuer.
const Name = "contabo"

// Default endpoints of the real Contabo API.
const (
	DefaultAPIURL  = "https://api.contabo.com"
	DefaultAuthURL = "https://auth.contabo.com/auth/realms/contabo/protocol/openid-connect/token"
)

// Options configures a Solver. Zero values get sensible defaults.
type Options struct {
	// APIURL and AuthURL are the Contabo endpoints. Tests point them at a
	// fake API.
	APIURL  string
	AuthURL string

	// AmbientCredentials are used when an issuer has no secretName and
	// cert-manager allows ambient credentials for it.
	AmbientCredentials *contabo.Credentials

	// OperationTimeout bounds one Present or CleanUp call, retries
	// included. It must stay below the Kubernetes API server's request
	// timeout (60s by default), because cert-manager calls the webhook
	// through it. When it runs out, the call fails and cert-manager tries
	// again later.
	OperationTimeout time.Duration
	// RequestTimeout bounds a single HTTP request to Contabo.
	RequestTimeout time.Duration
	// RateLimit is the most Contabo requests per second, per account.
	RateLimit float64
	// MaxRetries is how often a failed request is retried within one call.
	MaxRetries int
	// BaseBackoff is the first retry delay.
	BaseBackoff time.Duration
	UserAgent   string

	Logger *slog.Logger

	// KubeClient is used instead of the client built in Initialize. Tests
	// set it.
	KubeClient kubernetes.Interface
}

// Solver implements webhook.Solver for Contabo.
type Solver struct {
	opts  Options
	log   *slog.Logger
	cache *clientCache
	now   func() time.Time

	mu   sync.RWMutex
	kube kubernetes.Interface
}

var _ webhook.Solver = (*Solver)(nil)

// New creates a Solver.
func New(o Options) *Solver {
	if o.APIURL == "" {
		o.APIURL = DefaultAPIURL
	}
	if o.AuthURL == "" {
		o.AuthURL = DefaultAuthURL
	}
	if o.OperationTimeout <= 0 {
		o.OperationTimeout = 45 * time.Second
	}
	if o.RequestTimeout <= 0 {
		o.RequestTimeout = 15 * time.Second
	}
	if o.RateLimit <= 0 {
		o.RateLimit = 10
	}
	if o.MaxRetries < 0 {
		o.MaxRetries = 0
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	s := &Solver{opts: o, log: o.Logger, now: time.Now, kube: o.KubeClient}
	s.cache = newClientCache(s.newClient, func() time.Time { return s.now() })
	return s
}

func (s *Solver) newClient(creds contabo.Credentials) (*contabo.Client, error) {
	return contabo.New(contabo.Options{
		APIURL:         s.opts.APIURL,
		AuthURL:        s.opts.AuthURL,
		Credentials:    creds,
		RequestTimeout: s.opts.RequestTimeout,
		RateLimit:      s.opts.RateLimit,
		MaxRetries:     s.opts.MaxRetries,
		BaseBackoff:    s.opts.BaseBackoff,
		UserAgent:      s.opts.UserAgent,
		Logger:         s.log,
	})
}

// Name is the solverName in the issuer config.
func (s *Solver) Name() string { return Name }

// Initialize builds the Kubernetes client used to read credential Secrets.
// cert-manager calls it once when the webhook server starts.
func (s *Solver) Initialize(kubeClientConfig *rest.Config, _ <-chan struct{}) error {
	if s.opts.KubeClient != nil {
		return nil
	}
	if kubeClientConfig == nil {
		return errors.New("no Kubernetes client config")
	}
	cl, err := kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		return fmt.Errorf("build Kubernetes client: %w", err)
	}
	s.mu.Lock()
	s.kube = cl
	s.mu.Unlock()
	return nil
}

func (s *Solver) kubeClient() kubernetes.Interface {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.kube
}

// challenge is everything Present and CleanUp need for one request.
type challenge struct {
	fqdn         string
	resolvedZone string
	key          string
	cfg          Config
	acct         *account
	source       credentialSource
	log          *slog.Logger
}

// prepare checks the request, loads the config and credentials and picks
// the cached client for that account.
func (s *Solver) prepare(ctx context.Context, ch *v1alpha1.ChallengeRequest) (*challenge, error) {
	if ch == nil {
		return nil, errors.New("empty challenge request")
	}
	if ch.Key == "" {
		return nil, errors.New("the challenge request has no key")
	}
	fqdnRaw := ch.ResolvedFQDN
	if fqdnRaw == "" {
		// cert-manager always sends ResolvedFQDN. Fall back to the
		// standard name so a hand-made request still works.
		fqdnRaw = "_acme-challenge." + strings.TrimPrefix(ch.DNSName, "*.")
	}
	fqdn, err := normalizeName(fqdnRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid challenge name: %w", err)
	}
	if fqdn == "" || !strings.Contains(fqdn, ".") {
		return nil, fmt.Errorf("invalid challenge name %q: it must be a full domain name", fqdnRaw)
	}
	resolvedZone, err := normalizeName(ch.ResolvedZone)
	if err != nil {
		return nil, fmt.Errorf("invalid resolved zone %q: %w", ch.ResolvedZone, err)
	}

	cfg, err := parseConfig(ch.Config)
	if err != nil {
		return nil, err
	}
	creds, src, err := loadCredentials(ctx, s.kubeClient(), ch.ResourceNamespace, cfg,
		ch.AllowAmbientCredentials, s.opts.AmbientCredentials)
	if err != nil {
		return nil, err
	}
	acct, err := s.cache.get(creds)
	if err != nil {
		return nil, err
	}
	log := s.log.With("fqdn", fqdn, "dnsName", ch.DNSName, "namespace", ch.ResourceNamespace)
	if ch.UID != "" {
		log = log.With("uid", string(ch.UID))
	}
	return &challenge{
		fqdn: fqdn, resolvedZone: resolvedZone, key: ch.Key, cfg: cfg, acct: acct, source: src, log: log,
	}, nil
}

// Present creates the TXT record. It does nothing if the record is already
// there, and it never changes or removes other records.
func (s *Solver) Present(ch *v1alpha1.ChallengeRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), s.opts.OperationTimeout)
	defer cancel()

	c, err := s.prepare(ctx, ch)
	if err != nil {
		s.logRequestError(ch, "could not prepare the challenge", err)
		return fmt.Errorf("contabo: present: %w", err)
	}
	if err := s.present(ctx, c); err != nil {
		c.log.Error("could not present the challenge record", "error", err)
		return fmt.Errorf("contabo: present TXT %s: %w", c.fqdn, err)
	}
	return nil
}

func (s *Solver) present(ctx context.Context, c *challenge) error {
	zone, err := s.findZone(ctx, c)
	if err != nil {
		return err
	}
	log := c.log.With("zone", zone)

	matches, err := s.matchingRecords(ctx, c, zone)
	if err != nil {
		return err
	}
	if len(matches) > 0 {
		log.Info("challenge record already present", "recordID", int64(matches[0].ID))
		return nil
	}

	// The full name is sent, never the part relative to the zone: Contabo
	// treats any name that ends in the zone name as already full, so a
	// relative name such as "_acme-challenge.example.com" in the zone
	// example.com would land on the wrong record.
	rec, err := c.acct.client.CreateRecord(ctx, c.acct.apiName(zone), contabo.RecordRequest{
		Name: c.fqdn,
		Type: contabo.TypeTXT,
		TTL:  c.cfg.ttl(),
		Data: c.key,
	})
	switch {
	case contabo.IsConflict(err):
		// Another call created the same record first.
		log.Info("challenge record already present (the API reported a duplicate)")
		return nil
	case err != nil:
		return s.explain(c, err)
	}
	log.Info("challenge record created", "recordID", int64(rec.ID), "ttl", c.cfg.ttl())
	return nil
}

// CleanUp deletes the TXT record whose value is the challenge key, and only
// that one. A record that is already gone is not an error.
func (s *Solver) CleanUp(ch *v1alpha1.ChallengeRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), s.opts.OperationTimeout)
	defer cancel()

	c, err := s.prepare(ctx, ch)
	if err != nil {
		s.logRequestError(ch, "could not prepare the cleanup; cert-manager will try again", err)
		return fmt.Errorf("contabo: cleanup: %w", err)
	}
	if err := s.cleanUp(ctx, c); err != nil {
		c.log.Error("could not clean up the challenge record; cert-manager will try again", "error", err)
		return fmt.Errorf("contabo: clean up TXT %s: %w", c.fqdn, err)
	}
	return nil
}

func (s *Solver) cleanUp(ctx context.Context, c *challenge) error {
	zones, err := c.acct.listZones(ctx, s.now(), true)
	if err != nil {
		return s.explain(c, err)
	}
	// Look in every zone that could hold the name, not only the one
	// Present would pick today: the config or the zones may have changed
	// since the record was made.
	candidates := zonesContaining(c.fqdn, zones)
	if len(candidates) == 0 {
		c.log.Info("no Contabo zone contains the name, so there is nothing to clean up")
		return nil
	}

	var errs []error
	deleted := 0
	for _, zone := range candidates {
		matches, err := s.matchingRecords(ctx, c, zone)
		if contabo.IsNotFound(err) {
			continue // the zone was removed, and its records with it
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, r := range matches {
			if err := c.acct.client.DeleteRecord(ctx, c.acct.apiName(zone), int64(r.ID)); err != nil {
				errs = append(errs, fmt.Errorf("delete record %d in zone %s: %w", int64(r.ID), zone, s.explain(c, err)))
				continue
			}
			deleted++
			c.log.Info("challenge record deleted", "zone", zone, "recordID", int64(r.ID))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	if deleted == 0 {
		c.log.Info("challenge record was already gone")
	}
	return nil
}

// findZone picks the zone for Present. A miss in the cached zone list
// refreshes it once, so a zone added a moment ago is found.
func (s *Solver) findZone(ctx context.Context, c *challenge) (string, error) {
	zones, err := c.acct.listZones(ctx, s.now(), false)
	if err != nil {
		return "", s.explain(c, err)
	}
	zone, err := pickZone(c.fqdn, c.resolvedZone, c.cfg.Zone, zones)
	if err == nil {
		return zone, nil
	}
	if zones, err = c.acct.listZones(ctx, s.now(), true); err != nil {
		return "", s.explain(c, err)
	}
	return pickZone(c.fqdn, c.resolvedZone, c.cfg.Zone, zones)
}

// matchingRecords lists the TXT records in zone that hold the challenge
// key under the challenge name, in any of the spellings Contabo allows.
func (s *Solver) matchingRecords(ctx context.Context, c *challenge, zone string) ([]contabo.Record, error) {
	recs, err := c.acct.client.ListRecords(ctx, c.acct.apiName(zone))
	if contabo.IsNotFound(err) {
		c.acct.forgetZones()
		return nil, err
	}
	if err != nil {
		return nil, s.explain(c, err)
	}
	var out []contabo.Record
	for _, r := range recs {
		if strings.EqualFold(r.Type, contabo.TypeTXT) && recordFQDN(r.Name, zone) == c.fqdn && txtValue(r.Data) == c.key {
			out = append(out, r)
		}
	}
	return out, nil
}

// logRequestError logs an error that happened before the challenge was
// fully read, with whatever the request tells us.
func (s *Solver) logRequestError(ch *v1alpha1.ChallengeRequest, msg string, err error) {
	if ch == nil {
		s.log.Error(msg, "error", err)
		return
	}
	s.log.Error(msg, "fqdn", ch.ResolvedFQDN, "dnsName", ch.DNSName, "namespace", ch.ResourceNamespace, "error", err)
}

// explain adds a hint to errors a user can fix.
func (s *Solver) explain(c *challenge, err error) error {
	if errors.Is(err, contabo.ErrInvalidCredentials) {
		hint := "check all four values"
		switch {
		case strings.Contains(err.Error(), "invalid_grant"):
			hint = "the API user or API password is wrong"
		case strings.Contains(err.Error(), "unauthorized_client"), strings.Contains(err.Error(), "invalid_client"):
			hint = "the client ID or client secret is wrong"
		}
		return fmt.Errorf("Contabo rejected the credentials from %s (%s; see https://my.contabo.com/api/details): %w", c.source, hint, err) //nolint:staticcheck // the message starts with a name
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("the Contabo API did not finish in %s; cert-manager will try again: %w", s.opts.OperationTimeout, err)
	}
	return err
}
