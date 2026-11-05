package solver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/hayone1/cert-manager-webhook-contabo/internal/contabo"
	"github.com/hayone1/cert-manager-webhook-contabo/internal/contabo/contabotest"
)

const (
	ns         = "cert-manager"
	secretName = "contabo-credentials"
	key1       = "LoqXcYV8q5ONbJQxbmR7SCTNo3tiAXDfowyjxAjEuX0"
	key2       = "gkeAVWQbWt3jb2BA2B9iVPMZAKwiH5rOU8BAgGHc7QY"
)

type env struct {
	t    *testing.T
	fake *contabotest.Server
	kube *fake.Clientset
	s    *Solver
}

func newEnv(t *testing.T, zones ...string) *env {
	t.Helper()
	if len(zones) == 0 {
		zones = []string{"example.com"}
	}
	srv := contabotest.New()
	t.Cleanup(srv.Close)
	for _, z := range zones {
		srv.AddZone(z)
	}
	kube := fake.NewClientset(goodSecret(ns, secretName))
	e := &env{t: t, fake: srv, kube: kube}
	e.s = New(Options{
		APIURL:           srv.URL,
		AuthURL:          srv.TokenURL(),
		KubeClient:       kube,
		RateLimit:        1000,
		MaxRetries:       3,
		BaseBackoff:      time.Millisecond,
		OperationTimeout: 10 * time.Second,
		RequestTimeout:   5 * time.Second,
	})
	return e
}

func goodSecret(namespace, name string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Data: map[string][]byte{
			"client-id":     []byte(contabotest.ClientID),
			"client-secret": []byte(contabotest.ClientSecret),
			"api-user":      []byte(contabotest.APIUser),
			"api-password":  []byte(contabotest.APIPassword),
		},
	}
}

func rawConfig(t *testing.T, v any) *extapi.JSON {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return &extapi.JSON{Raw: b}
}

// req builds a challenge request like cert-manager sends it.
func req(t *testing.T, key, fqdn, zone string, cfg any) *v1alpha1.ChallengeRequest {
	t.Helper()
	if cfg == nil {
		cfg = map[string]any{"secretName": secretName}
	}
	return &v1alpha1.ChallengeRequest{
		UID:               "uid-1",
		Type:              "dns-01",
		DNSName:           strings.TrimPrefix(strings.TrimSuffix(fqdn, "."), "_acme-challenge."),
		Key:               key,
		ResourceNamespace: ns,
		ResolvedFQDN:      fqdn,
		ResolvedZone:      zone,
		Config:            rawConfig(t, cfg),
	}
}

func (e *env) values(zone, name string) []string {
	e.t.Helper()
	return e.fake.TXTValues(zone, name)
}

func (e *env) seedTXT(zone, name, data string) int64 {
	ttl, prio := 60.0, 0.0
	return e.fake.Seed(zone, contabotest.Record{Name: &name, Type: "TXT", TTL: &ttl, Prio: &prio, Data: data})
}

func mustOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func mustFail(t *testing.T, err error, contains ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, c := range contains {
		if !strings.Contains(err.Error(), c) {
			t.Fatalf("error %q does not contain %q", err, c)
		}
	}
}

func TestPresentAndCleanUp(t *testing.T) {
	e := newEnv(t)
	ch := req(t, key1, "_acme-challenge.example.com.", "example.com.", nil)

	mustOK(t, e.s.Present(ch))
	if got := e.values("example.com", "_acme-challenge.example.com"); !slices.Equal(got, []string{key1}) {
		t.Fatalf("TXT values = %v", got)
	}
	var rec contabotest.Record
	for _, r := range e.fake.Records("example.com") {
		if r.Type == "TXT" {
			rec = r
		}
	}
	if rec.TTL == nil || *rec.TTL != DefaultTTL {
		t.Fatalf("TTL = %v, want %d", rec.TTL, DefaultTTL)
	}
	if rec.Name == nil || *rec.Name != "_acme-challenge.example.com" {
		t.Fatalf("record should be stored under its full name, got %v", rec.Name)
	}

	// Present again: nothing new is created.
	mustOK(t, e.s.Present(ch))
	if n := e.fake.Calls("create"); n != 1 {
		t.Fatalf("create called %d times, want 1", n)
	}

	mustOK(t, e.s.CleanUp(ch))
	if got := e.values("example.com", "_acme-challenge.example.com"); len(got) != 0 {
		t.Fatalf("record not removed: %v", got)
	}
	// CleanUp again: the record is already gone, which is fine.
	mustOK(t, e.s.CleanUp(ch))
}

func TestEveryNameFormIsRecognized(t *testing.T) {
	for _, name := range []string{
		"_acme-challenge",
		"_acme-challenge.example.com",
		"_acme-challenge.example.com.",
		"_ACME-Challenge.Example.COM",
		" _acme-challenge ",
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.seedTXT("example.com", name, key1)
			ch := req(t, key1, "_acme-challenge.example.com.", "example.com.", nil)
			mustOK(t, e.s.Present(ch))
			if n := e.fake.Calls("create"); n != 0 {
				t.Fatalf("Present created a duplicate of %q", name)
			}
			mustOK(t, e.s.CleanUp(ch))
			if got := e.values("example.com", "_acme-challenge.example.com"); len(got) != 0 {
				t.Fatalf("record %q was not removed", name)
			}
		})
	}
}

func TestQuotedAndSplitTXTData(t *testing.T) {
	for _, data := range []string{`"` + key1 + `"`, ` "` + key1[:20] + `" "` + key1[20:] + `" `} {
		e := newEnv(t)
		e.seedTXT("example.com", "_acme-challenge", data)
		ch := req(t, key1, "_acme-challenge.example.com.", "example.com.", nil)
		mustOK(t, e.s.Present(ch))
		if n := e.fake.Calls("create"); n != 0 {
			t.Fatalf("%s: quoted value not recognized", data)
		}
		mustOK(t, e.s.CleanUp(ch))
		if n := len(e.fake.Records("example.com")); n != 4 {
			t.Fatalf("%s: record not removed, zone has %d records", data, n)
		}
	}
}

func TestSeveralValuesOnOneName(t *testing.T) {
	e := newEnv(t)
	keep := e.seedTXT("example.com", "_acme-challenge", "not-ours")
	apex := req(t, key1, "_acme-challenge.example.com.", "example.com.", nil)
	wild := req(t, key2, "_acme-challenge.example.com.", "example.com.", nil)
	wild.DNSName = "*.example.com"

	mustOK(t, e.s.Present(apex))
	mustOK(t, e.s.Present(wild))
	got := e.values("example.com", "_acme-challenge.example.com")
	slices.Sort(got)
	want := []string{key2, key1, "not-ours"}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("values = %v, want %v", got, want)
	}

	mustOK(t, e.s.CleanUp(wild))
	got = e.values("example.com", "_acme-challenge.example.com")
	slices.Sort(got)
	if !slices.Equal(got, []string{key1, "not-ours"}) {
		t.Fatalf("CleanUp removed the wrong record: %v", got)
	}
	mustOK(t, e.s.CleanUp(apex))
	if got := e.values("example.com", "_acme-challenge.example.com"); !slices.Equal(got, []string{"not-ours"}) {
		t.Fatalf("values = %v", got)
	}
	found := false
	for _, r := range e.fake.Records("example.com") {
		found = found || r.RecordID == keep
	}
	if !found {
		t.Fatal("a record that is not ours was deleted")
	}
}

func TestConflictOnCreateIsSuccess(t *testing.T) {
	e := newEnv(t)
	e.fake.Inject(contabotest.Fault{Method: http.MethodPost, Path: "/records", Status: http.StatusConflict, Count: 1})
	mustOK(t, e.s.Present(req(t, key1, "_acme-challenge.example.com.", "example.com.", nil)))
}

func TestConcurrentPresent(t *testing.T) {
	e := newEnv(t)
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := range 20 {
		key := key1
		if i%2 == 1 {
			key = key2
		}
		wg.Go(func() { errs <- e.s.Present(req(t, key, "_acme-challenge.example.com.", "example.com.", nil)) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		mustOK(t, err)
	}
	got := e.values("example.com", "_acme-challenge.example.com")
	slices.Sort(got)
	want := []string{key1, key2}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("values = %v, want exactly one record per key", got)
	}
}

func TestCleanUpRemovesDuplicates(t *testing.T) {
	e := newEnv(t)
	e.seedTXT("example.com", "_acme-challenge", key1)
	e.seedTXT("example.com", "_acme-challenge.example.com.", `"`+key1+`"`)
	mustOK(t, e.s.CleanUp(req(t, key1, "_acme-challenge.example.com.", "example.com.", nil)))
	if got := e.values("example.com", "_acme-challenge.example.com"); len(got) != 0 {
		t.Fatalf("duplicates left: %v", got)
	}
}

func TestCleanUpDuringDeleteOutage(t *testing.T) {
	e := newEnv(t)
	ch := req(t, key1, "_acme-challenge.example.com.", "example.com.", nil)
	mustOK(t, e.s.Present(ch))

	// Short outage: the client's own retries get through it.
	e.fake.Inject(contabotest.Fault{Method: http.MethodDelete, Path: "/records/", Status: 500, Count: 2})
	ch2 := req(t, key2, "_acme-challenge.example.com.", "example.com.", nil)
	mustOK(t, e.s.Present(ch2))
	mustOK(t, e.s.CleanUp(ch2))

	// Long outage: CleanUp fails (so cert-manager tries again later) and
	// the record is still there.
	e.fake.Inject(contabotest.Fault{Method: http.MethodDelete, Path: "/records/", Status: 500, Count: 1000})
	err := e.s.CleanUp(ch)
	mustFail(t, err, "HTTP 500")
	if got := e.values("example.com", "_acme-challenge.example.com"); !slices.Equal(got, []string{key1}) {
		t.Fatalf("values = %v", got)
	}
	// The outage ends; the next CleanUp works.
	e.fake.ClearFaults()
	mustOK(t, e.s.CleanUp(ch))
	if got := e.values("example.com", "_acme-challenge.example.com"); len(got) != 0 {
		t.Fatalf("values = %v", got)
	}
}

func TestRateLimitIsRetried(t *testing.T) {
	e := newEnv(t)
	e.fake.Inject(contabotest.Fault{Status: http.StatusTooManyRequests, Count: 2, Header: http.Header{"Retry-After": {"0"}}})
	mustOK(t, e.s.Present(req(t, key1, "_acme-challenge.example.com.", "example.com.", nil)))
}

func TestMissingZone(t *testing.T) {
	e := newEnv(t)
	ch := req(t, key1, "_acme-challenge.other.org.", "other.org.", nil)
	mustFail(t, e.s.Present(ch), "no zone that contains", "example.com")
	mustOK(t, e.s.CleanUp(ch)) // nothing to clean up
}

func TestZoneAddedLaterIsFound(t *testing.T) {
	e := newEnv(t)
	ch := req(t, key1, "_acme-challenge.new.org.", "new.org.", nil)
	mustFail(t, e.s.Present(ch), "no zone")
	e.fake.AddZone("new.org")
	mustOK(t, e.s.Present(ch)) // the cached zone list is refreshed on a miss
}

func TestZoneRemovedDuringCleanUp(t *testing.T) {
	e := newEnv(t)
	ch := req(t, key1, "_acme-challenge.example.com.", "example.com.", nil)
	mustOK(t, e.s.Present(ch))
	e.fake.Inject(contabotest.Fault{Method: http.MethodGet, Path: "/records", Status: http.StatusNotFound, Count: 1})
	mustOK(t, e.s.CleanUp(ch))
}

func TestSubZoneWins(t *testing.T) {
	e := newEnv(t, "example.com", "sub.example.com")
	ch := req(t, key1, "_acme-challenge.www.sub.example.com.", "sub.example.com.", nil)
	mustOK(t, e.s.Present(ch))
	if got := e.values("sub.example.com", "_acme-challenge.www.sub.example.com"); !slices.Equal(got, []string{key1}) {
		t.Fatalf("sub zone values = %v", got)
	}
	if n := len(e.fake.Records("example.com")); n != 4 {
		t.Fatalf("parent zone was changed: %d records", n)
	}

	// Without a resolved zone, the longest match is used.
	ch2 := req(t, key2, "_acme-challenge.www.sub.example.com.", "", nil)
	mustOK(t, e.s.Present(ch2))
	if got := e.values("sub.example.com", "_acme-challenge.www.sub.example.com"); len(got) != 2 {
		t.Fatalf("sub zone values = %v", got)
	}
}

func TestResolvedZoneMismatch(t *testing.T) {
	// DNS says sub.example.com is its own zone (delegated elsewhere), but the
	// Contabo account only has example.com: the record would be invisible.
	e := newEnv(t)
	ch := req(t, key1, "_acme-challenge.www.sub.example.com.", "sub.example.com.", nil)
	mustFail(t, e.s.Present(ch), "public DNS says", "ns1.contabo.net")
	if n := e.fake.Calls("create"); n != 0 {
		t.Fatal("nothing should be created")
	}

	// The zone setting overrides the check.
	forced := req(t, key1, "_acme-challenge.www.sub.example.com.", "sub.example.com.",
		map[string]any{"secretName": secretName, "zone": "Example.COM."})
	mustOK(t, e.s.Present(forced))
	if got := e.values("example.com", "_acme-challenge.www.sub.example.com"); !slices.Equal(got, []string{key1}) {
		t.Fatalf("values = %v", got)
	}
	// CleanUp finds it even without the override.
	mustOK(t, e.s.CleanUp(ch))
	if got := e.values("example.com", "_acme-challenge.www.sub.example.com"); len(got) != 0 {
		t.Fatalf("values = %v", got)
	}

	bad := req(t, key1, "_acme-challenge.example.com.", "example.com.", map[string]any{"secretName": secretName, "zone": "other.org"})
	mustFail(t, e.s.Present(bad), "not in this Contabo account")
	e.fake.AddZone("other.org")
	mustFail(t, e.s.Present(bad), "does not contain")
}

func TestCNAMEDelegatedChallenge(t *testing.T) {
	// With cnameStrategy: Follow, cert-manager sends the CNAME target.
	e := newEnv(t, "acme-delegation.example.com")
	ch := req(t, key1, "abc123.acme-delegation.example.com.", "acme-delegation.example.com.", nil)
	ch.DNSName = "customer.example.net"
	mustOK(t, e.s.Present(ch))
	if got := e.values("acme-delegation.example.com", "abc123.acme-delegation.example.com"); !slices.Equal(got, []string{key1}) {
		t.Fatalf("values = %v", got)
	}

	// A CNAME may even point at a zone apex.
	apex := req(t, key2, "acme-delegation.example.com.", "acme-delegation.example.com.", nil)
	mustOK(t, e.s.Present(apex))
	if got := e.values("acme-delegation.example.com", "acme-delegation.example.com"); !slices.Equal(got, []string{key2}) {
		t.Fatalf("apex values = %v", got)
	}
	mustOK(t, e.s.CleanUp(apex))
}

func TestNameEndingInZoneNameTwice(t *testing.T) {
	// Sent relative ("_acme-challenge.example.com"), Contabo would treat this
	// name as already full and write the wrong record.
	e := newEnv(t)
	ch := req(t, key1, "_acme-challenge.example.com.example.com.", "example.com.", nil)
	mustOK(t, e.s.Present(ch))
	if got := e.values("example.com", "_acme-challenge.example.com.example.com"); !slices.Equal(got, []string{key1}) {
		t.Fatalf("values = %v", got)
	}
	if got := e.values("example.com", "_acme-challenge.example.com"); len(got) != 0 {
		t.Fatalf("record landed on the wrong name: %v", got)
	}
}

func TestInternationalDomain(t *testing.T) {
	e := newEnv(t, "xn--bcher-kva.example")
	ch := req(t, key1, "_acme-challenge.BÜCHER.example.", "bücher.example.", nil)
	mustOK(t, e.s.Present(ch))
	if got := e.values("xn--bcher-kva.example", "_acme-challenge.xn--bcher-kva.example"); !slices.Equal(got, []string{key1}) {
		t.Fatalf("values = %v", got)
	}
}

func TestFallbackToDNSName(t *testing.T) {
	e := newEnv(t)
	ch := req(t, key1, "", "", nil)
	ch.DNSName = "*.example.com"
	mustOK(t, e.s.Present(ch))
	if got := e.values("example.com", "_acme-challenge.example.com"); !slices.Equal(got, []string{key1}) {
		t.Fatalf("values = %v", got)
	}
}

func TestBadRequests(t *testing.T) {
	e := newEnv(t)
	mustFail(t, e.s.Present(nil), "empty challenge request")
	mustFail(t, e.s.Present(req(t, "", "_acme-challenge.example.com.", "example.com.", nil)), "no key")
	mustFail(t, e.s.Present(req(t, key1, "_acme..example.com.", "example.com.", nil)), "invalid challenge name")
	mustFail(t, e.s.Present(req(t, key1, "localhost.", "", nil)), "invalid challenge name")
}

func TestBadCredentials(t *testing.T) {
	cases := map[string]struct {
		key, value, hint string
	}{
		"password":      {"api-password", "Pa55-Value-XYZ", "API user or API password is wrong"},
		"user":          {"api-user", "nobody@example.com", "API user or API password is wrong"},
		"client secret": {"client-secret", "Cl1ent-Value-XYZ", "client ID or client secret is wrong"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			s := goodSecret(ns, "bad")
			s.Data[c.key] = []byte(c.value)
			_, _ = e.kube.CoreV1().Secrets(ns).Create(context.Background(), s, metav1.CreateOptions{})
			err := e.s.Present(req(t, key1, "_acme-challenge.example.com.", "example.com.", map[string]any{"secretName": "bad"}))
			mustFail(t, err, c.hint, "Secret cert-manager/bad")
			if strings.Contains(err.Error(), c.value) && c.key != "api-user" {
				t.Fatalf("error leaks the secret value: %v", err)
			}
			if !strings.Contains(err.Error(), "credentials were rejected") {
				t.Fatalf("missing cause: %v", err)
			}
		})
	}
}

func TestSecretProblems(t *testing.T) {
	e := newEnv(t)
	ch := func(cfg map[string]any) *v1alpha1.ChallengeRequest {
		return req(t, key1, "_acme-challenge.example.com.", "example.com.", cfg)
	}
	mustFail(t, e.s.Present(ch(map[string]any{"secretName": "nope"})), "Secret cert-manager/nope not found")

	partial := goodSecret(ns, "partial")
	delete(partial.Data, "api-password")
	partial.Data["client-secret"] = []byte("\n")
	_, _ = e.kube.CoreV1().Secrets(ns).Create(context.Background(), partial, metav1.CreateOptions{})
	mustFail(t, e.s.Present(ch(map[string]any{"secretName": "partial"})), "missing these keys or they are empty: api-password, client-secret")

	// Values with a line break at the end (from "echo") still work.
	nl := goodSecret(ns, "newlines")
	for k, v := range nl.Data {
		nl.Data[k] = append(v, '\n')
	}
	nl.Data["client-id"] = []byte("  " + contabotest.ClientID + " \r\n")
	_, _ = e.kube.CoreV1().Secrets(ns).Create(context.Background(), nl, metav1.CreateOptions{})
	mustOK(t, e.s.Present(ch(map[string]any{"secretName": "newlines"})))

	// Custom key names.
	custom := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "custom"},
		Data: map[string][]byte{
			"id": []byte(contabotest.ClientID), "secret": []byte(contabotest.ClientSecret),
			"user": []byte(contabotest.APIUser), "password": []byte(contabotest.APIPassword),
		},
	}
	_, _ = e.kube.CoreV1().Secrets(ns).Create(context.Background(), custom, metav1.CreateOptions{})
	mustOK(t, e.s.Present(req(t, key2, "_acme-challenge.example.com.", "example.com.", map[string]any{
		"secretName": "custom",
		"secretKeys": map[string]string{"clientID": "id", "clientSecret": "secret", "apiUser": "user", "apiPassword": "password"},
	})))

	// No RBAC to read the Secret.
	e.kube.PrependReactor("get", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "locked", fmt.Errorf("rbac"))
	})
	mustFail(t, e.s.Present(ch(map[string]any{"secretName": "locked"})), "not allowed to read Secret cert-manager/locked", "secretNames")

	// The Issuer's namespace is used, never another one.
	other := ch(map[string]any{"secretName": secretName})
	other.ResourceNamespace = ""
	mustFail(t, e.s.Present(other), "no namespace")
}

func TestAmbientCredentials(t *testing.T) {
	e := newEnv(t)
	noSecret := func(allow bool) *v1alpha1.ChallengeRequest {
		r := req(t, key1, "_acme-challenge.example.com.", "example.com.", map[string]any{})
		r.AllowAmbientCredentials = allow
		return r
	}
	mustFail(t, e.s.Present(noSecret(true)), "no secretName")

	e.s.opts.AmbientCredentials = &contabo.Credentials{
		ClientID: contabotest.ClientID, ClientSecret: contabotest.ClientSecret,
		APIUser: contabotest.APIUser, APIPassword: contabotest.APIPassword,
	}
	mustFail(t, e.s.Present(noSecret(false)), "does not allow ambient credentials")
	mustOK(t, e.s.Present(noSecret(true)))

	nilCfg := noSecret(true)
	nilCfg.Config = nil
	mustOK(t, e.s.Present(nilCfg))
}

func TestClientIsCachedPerAccount(t *testing.T) {
	e := newEnv(t)
	ch := req(t, key1, "_acme-challenge.example.com.", "example.com.", nil)
	for range 3 {
		mustOK(t, e.s.Present(ch))
	}
	mustOK(t, e.s.CleanUp(ch))
	if n := e.fake.Calls("token"); n != 1 {
		t.Fatalf("token requested %d times, want 1 (cached client)", n)
	}
	if n := e.fake.Calls("listZones"); n != 2 {
		// One for the Presents (cached for a minute) and one for CleanUp,
		// which always looks at a fresh list.
		t.Fatalf("listZones called %d times, want 2", n)
	}

	// A rotated Secret gets a new client and token at once.
	s := goodSecret(ns, secretName)
	s.Data["client-id"] = []byte(contabotest.ClientID + "\n")
	_, _ = e.kube.CoreV1().Secrets(ns).Update(context.Background(), s, metav1.UpdateOptions{})
	mustOK(t, e.s.Present(ch))
	if n := e.fake.Calls("token"); n != 1 {
		t.Fatalf("same values after trimming must reuse the client; token calls = %d", n)
	}
	s.Data["api-password"] = []byte("rotated")
	_, _ = e.kube.CoreV1().Secrets(ns).Update(context.Background(), s, metav1.UpdateOptions{})
	mustFail(t, e.s.Present(ch), "credentials were rejected")
}

func TestCacheEvictsIdleClients(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	e.s.now = func() time.Time { return now }
	ch := req(t, key1, "_acme-challenge.example.com.", "example.com.", nil)
	mustOK(t, e.s.Present(ch))
	now = now.Add(2 * clientIdleTTL)
	mustOK(t, e.s.Present(ch))
	if n := e.fake.Calls("token"); n != 2 {
		t.Fatalf("idle client was not dropped: token calls = %d", n)
	}
	if len(e.s.cache.entries) != 1 {
		t.Fatalf("cache has %d entries", len(e.s.cache.entries))
	}
}

func TestCacheSizeIsBounded(t *testing.T) {
	n := 0
	cc := newClientCache(func(contabo.Credentials) (*contabo.Client, error) {
		n++
		return contabo.New(contabo.Options{APIURL: "https://api.contabo.invalid"})
	}, time.Now)
	for i := range maxClients + 10 {
		if _, err := cc.get(contabo.Credentials{ClientID: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if len(cc.entries) != maxClients {
		t.Fatalf("cache has %d entries, want %d", len(cc.entries), maxClients)
	}
}

func TestOperationTimeout(t *testing.T) {
	e := newEnv(t)
	e.s.opts.OperationTimeout = 300 * time.Millisecond
	e.fake.Inject(contabotest.Fault{Path: "/v1/dns", Status: 503, Count: 1000, Header: http.Header{"Retry-After": {"20"}}})
	start := time.Now()
	err := e.s.Present(req(t, key1, "_acme-challenge.example.com.", "example.com.", nil))
	mustFail(t, err, "did not finish")
	if time.Since(start) > 5*time.Second {
		t.Fatal("timeout was not honored")
	}
}

func TestCustomTTL(t *testing.T) {
	e := newEnv(t)
	mustOK(t, e.s.Present(req(t, key1, "_acme-challenge.example.com.", "example.com.",
		map[string]any{"secretName": secretName, "ttl": "300"})))
	for _, r := range e.fake.Records("example.com") {
		if r.Type == "TXT" && *r.TTL != 300 {
			t.Fatalf("TTL = %v", *r.TTL)
		}
	}
}

func TestInvalidConfig(t *testing.T) {
	e := newEnv(t)
	for cfg, want := range map[string]string{
		`{"secretNme":"x"}`:                                 `unknown field "secretNme"`,
		`{"secretName":"x","ttl":0}`:                        "ttl must be between",
		`{"secretName":"x","ttl":"soon"}`:                   "not a whole number",
		`{"secretName":"Not_Valid"}`:                        "not a valid Kubernetes Secret name",
		`{"secretName":"x","zone":"a..b"}`:                  "not a valid domain name",
		`{"secretName":"x","secretKeys":{"apiUser":"a b"}}`: "not a valid Secret key",
		`[1,2]`:                 "invalid webhook config",
		`{"secretName":"x"} {}`: "unexpected data",
		`{"secretName":"x","apiURL":"http://evil"}`: `unknown field "apiURL"`,
	} {
		r := req(t, key1, "_acme-challenge.example.com.", "example.com.", nil)
		r.Config = &extapi.JSON{Raw: []byte(cfg)}
		mustFail(t, e.s.Present(r), want)
		mustFail(t, e.s.CleanUp(r), want)
	}
}

func TestInitializeBuildsKubeClient(t *testing.T) {
	s := New(Options{})
	if err := s.Initialize(nil, nil); err == nil {
		t.Fatal("nil config should fail")
	}
	if s.Name() != "contabo" {
		t.Fatalf("Name() = %q", s.Name())
	}
}
