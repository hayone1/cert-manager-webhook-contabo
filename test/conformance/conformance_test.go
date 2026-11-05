//go:build conformance

// Package conformance runs cert-manager's DNS-01 webhook conformance suite
// (github.com/cert-manager/cert-manager/test/acme) against the solver.
//
// It needs the envtest binaries (etcd, kube-apiserver, kubectl) in the
// TEST_ASSET_ETCD, TEST_ASSET_KUBE_APISERVER and TEST_ASSET_KUBECTL
// environment variables. `make conformance` downloads them and sets them.
// The suite's packages panic at start when the binaries are missing, so
// this file is behind the "conformance" build tag and `go test ./...`
// skips it.
//
// Two modes:
//
//   - fake (default): the fake Contabo API plus its DNS server. No account.
//   - real (CONFORMANCE_MODE=real): the real Contabo API and a real zone
//     (CONFORMANCE_ZONE), checked against Contabo's name servers. Needs the
//     CONTABO_* credentials. Local only, never in CI.
package conformance

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	acmetest "github.com/cert-manager/cert-manager/test/acme"

	"github.com/hayone1/cert-manager-webhook-contabo/internal/contabo/contabotest"
	"github.com/hayone1/cert-manager-webhook-contabo/internal/solver"
)

func requireEnvtest(t *testing.T) {
	t.Helper()
	for _, k := range []string{"TEST_ASSET_ETCD", "TEST_ASSET_KUBE_APISERVER", "TEST_ASSET_KUBECTL"} {
		if os.Getenv(k) == "" {
			t.Skipf("%s is not set; run `make conformance` to download the envtest binaries", k)
		}
	}
}

func logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestConformanceFake(t *testing.T) {
	requireEnvtest(t)
	if os.Getenv("CONFORMANCE_MODE") == "real" {
		t.Skip("CONFORMANCE_MODE=real: running the real-API test only")
	}

	fake := contabotest.New()
	defer fake.Close()
	fake.AddZone("example.com")
	dns, err := fake.StartDNS("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer dns.Close()

	s := solver.New(solver.Options{
		APIURL:      fake.URL,
		AuthURL:     fake.TokenURL(),
		BaseBackoff: 10 * time.Millisecond,
		Logger:      logger(),
	})
	fixture := acmetest.NewFixture(s,
		acmetest.SetResolvedZone("example.com."),
		acmetest.SetResolvedFQDN("_acme-challenge.example.com."),
		acmetest.SetAllowAmbientCredentials(false),
		acmetest.SetManifestPath("testdata/fake"),
		acmetest.SetConfig(map[string]any{"secretName": "contabo-credentials"}),
		acmetest.SetDNSServer(dns.Addr()),
		acmetest.SetUseAuthoritative(false),
		acmetest.SetStrict(true),
		acmetest.SetPollInterval(500*time.Millisecond),
		acmetest.SetPropagationLimit(30*time.Second),
	)
	// RunConformance nests a second control plane inside the first, so the
	// two halves are run one after the other instead (as webhook-example does).
	fixture.RunBasic(t)
	fixture.RunExtended(t)

	if n := len(fake.TXTValues("example.com", "_acme-challenge.example.com")); n != 0 {
		t.Fatalf("%d challenge records left behind", n)
	}
}

// TestConformanceReal runs the same suite against the real Contabo API.
// Every record lives under cm-conf-<id>.<zone> and is removed by the suite.
func TestConformanceReal(t *testing.T) {
	if os.Getenv("CONFORMANCE_MODE") != "real" {
		t.Skip("set CONFORMANCE_MODE=real to run against the real Contabo API")
	}
	requireEnvtest(t)
	zone := strings.TrimSuffix(strings.ToLower(os.Getenv("CONFORMANCE_ZONE")), ".")
	if zone == "" {
		t.Fatal("set CONFORMANCE_ZONE to a zone that exists in your Contabo account")
	}
	creds := map[string]string{}
	for key, env := range map[string]string{
		"client-id": "CONTABO_CLIENT_ID", "client-secret": "CONTABO_CLIENT_SECRET",
		"api-user": "CONTABO_API_USER", "api-password": "CONTABO_API_PASSWORD",
	} {
		v := os.Getenv(env)
		if v == "" {
			t.Fatalf("%s is not set", env)
		}
		creds[key] = v
	}

	// The suite reads its Secret from a manifest directory, so write one
	// to a temporary directory that is removed afterwards.
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Secret\nmetadata:\n  name: contabo-credentials\ntype: Opaque\nstringData:\n")
	for _, k := range []string{"client-id", "client-secret", "api-user", "api-password"} {
		fmt.Fprintf(&b, "  %s: %q\n", k, creds[k])
	}
	if err := os.WriteFile(filepath.Join(dir, "account.yaml"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	id := os.Getenv("CONFORMANCE_ID")
	if id == "" {
		id = fmt.Sprintf("%x", time.Now().UnixNano()&0xffffff)
	}
	dnsServer := os.Getenv("CONFORMANCE_DNS_SERVER")
	if dnsServer == "" {
		dnsServer = "1.1.1.1:53"
	}
	s := solver.New(solver.Options{Logger: logger()})
	fixture := acmetest.NewFixture(s,
		acmetest.SetResolvedZone(zone+"."),
		acmetest.SetResolvedFQDN(fmt.Sprintf("_acme-challenge.cm-conf-%s.%s.", id, zone)),
		acmetest.SetDNSName(fmt.Sprintf("cm-conf-%s.%s", id, zone)),
		acmetest.SetAllowAmbientCredentials(false),
		acmetest.SetManifestPath(dir),
		acmetest.SetConfig(map[string]any{"secretName": "contabo-credentials", "ttl": 60}),
		// Find the zone's name servers through a public resolver, then ask
		// ns1/ns2/ns3.contabo.net directly, like cert-manager does.
		acmetest.SetDNSServer(dnsServer),
		acmetest.SetUseAuthoritative(true),
		acmetest.SetStrict(true),
		acmetest.SetPollInterval(5*time.Second),
		acmetest.SetPropagationLimit(15*time.Minute),
	)
	fixture.RunBasic(t)
	fixture.RunExtended(t)
}
