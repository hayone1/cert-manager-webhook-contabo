package contabotest_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/miekg/dns"

	"github.com/hayone1/cert-manager-webhook-contabo/internal/contabo/contabotest"
)

func query(t *testing.T, addr, name string, qtype uint16, network string) *dns.Msg {
	t.Helper()
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	c := &dns.Client{Net: network}
	r, _, err := c.Exchange(m, addr)
	if err != nil {
		t.Fatalf("%s %s: %v", name, dns.TypeToString[qtype], err)
	}
	return r
}

func TestFakeDNS(t *testing.T) {
	srv := contabotest.New()
	defer srv.Close()
	srv.AddZone("example.com")
	srv.AddZone("sub.example.com")
	d, err := srv.StartDNS("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	ttl, prio := 60.0, 0.0
	seed := func(zone, name, typ, data string) {
		srv.Seed(zone, contabotest.Record{Name: &name, Type: typ, TTL: &ttl, Prio: &prio, Data: data})
	}
	seed("example.com", "_acme-challenge", "TXT", "plain")
	seed("example.com", "_acme-challenge.example.com.", "TXT", `"quoted"`)
	seed("example.com", "deep.a.b", "TXT", strings.Repeat("x", 300))
	seed("example.com", "www", "A", "1.1.1.1")
	seed("example.com", "alias", "CNAME", "www.example.com")

	for _, network := range []string{"udp", "tcp"} {
		r := query(t, d.Addr(), "_ACME-challenge.example.com", dns.TypeTXT, network)
		var got []string
		for _, rr := range r.Answer {
			got = append(got, strings.Join(rr.(*dns.TXT).Txt, ""))
		}
		if !r.Authoritative || len(got) != 2 || !strings.Contains(strings.Join(got, ","), "plain") || !strings.Contains(strings.Join(got, ","), "quoted") {
			t.Fatalf("%s TXT answer: %v", network, r)
		}
	}

	r := query(t, d.Addr(), "deep.a.b.example.com", dns.TypeTXT, "tcp")
	if txt := r.Answer[0].(*dns.TXT).Txt; len(txt) != 2 || len(txt[0]) != 255 {
		t.Fatalf("long TXT should be split: %v", txt)
	}

	r = query(t, d.Addr(), "example.com", dns.TypeSOA, "udp")
	if soa, ok := r.Answer[0].(*dns.SOA); !ok || soa.Hdr.Name != "example.com." || soa.Minttl != 3600 {
		t.Fatalf("SOA: %v", r)
	}
	r = query(t, d.Addr(), "sub.example.com", dns.TypeSOA, "udp")
	if soa, ok := r.Answer[0].(*dns.SOA); !ok || soa.Hdr.Name != "sub.example.com." {
		t.Fatalf("child zone SOA: %v", r)
	}
	r = query(t, d.Addr(), "example.com", dns.TypeNS, "udp")
	if len(r.Answer) != 3 {
		t.Fatalf("NS: %v", r)
	}

	// Below the apex, SOA is in the authority section, not the answer.
	r = query(t, d.Addr(), "www.example.com", dns.TypeSOA, "udp")
	if r.Rcode != dns.RcodeSuccess || len(r.Answer) != 0 || len(r.Ns) != 1 {
		t.Fatalf("NODATA: %v", r)
	}
	r = query(t, d.Addr(), "missing.example.com", dns.TypeTXT, "udp")
	if r.Rcode != dns.RcodeNameError {
		t.Fatalf("missing name should be NXDOMAIN: %v", r)
	}
	r = query(t, d.Addr(), "a.b.example.com", dns.TypeTXT, "udp")
	if r.Rcode != dns.RcodeSuccess {
		t.Fatalf("empty non-terminal should be NOERROR: %v", r)
	}
	r = query(t, d.Addr(), "alias.example.com", dns.TypeTXT, "udp")
	if c, ok := r.Answer[0].(*dns.CNAME); !ok || c.Target != "www.example.com." {
		t.Fatalf("CNAME: %v", r)
	}
	r = query(t, d.Addr(), "www.example.com", dns.TypeA, "udp")
	if a, ok := r.Answer[0].(*dns.A); !ok || a.A.String() != "1.1.1.1" {
		t.Fatalf("A: %v", r)
	}
	r = query(t, d.Addr(), "example.org", dns.TypeSOA, "udp")
	if r.Rcode != dns.RcodeNameError {
		t.Fatalf("outside every zone: %v", r)
	}

	if got := srv.TXTValues("example.com", "_acme-challenge.example.com."); len(got) != 2 {
		t.Fatalf("TXTValues = %v", got)
	}
}

func TestFakeDuplicateTXT(t *testing.T) {
	srv := contabotest.New()
	defer srv.Close()
	srv.AddZone("example.com")
	post := func(name, data string) int {
		t.Helper()
		resp, err := srv.Client().Post(srv.URL+"/token", "application/x-www-form-urlencoded",
			strings.NewReader("grant_type=password&client_id=INT-test&client_secret=test-secret&username=user%40example.com&password=test-password"))
		if err != nil {
			t.Fatal(err)
		}
		var tok struct {
			AccessToken string `json:"access_token"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&tok)
		_ = resp.Body.Close()
		body := `{"name":` + strconv.Quote(name) + `,"type":"TXT","ttl":60,"prio":0,"data":` + strconv.Quote(data) + `}`
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/dns/zones/example.com/records", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
		req.Header.Set("x-request-id", "test")
		req.Header.Set("Content-Type", "application/json")
		r, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = r.Body.Close()
		return r.StatusCode
	}
	if c := post("_acme-challenge.example.com", "abc"); c != http.StatusCreated {
		t.Fatalf("first create: %d", c)
	}
	for _, dup := range [][2]string{
		{"_acme-challenge", "abc"},
		{"_ACME-Challenge.example.com.", "abc"},
		{"_acme-challenge.example.com", `"abc"`},
	} {
		if c := post(dup[0], dup[1]); c != http.StatusConflict {
			t.Errorf("duplicate %q %q: got %d, want 409", dup[0], dup[1], c)
		}
	}
	if c := post("_acme-challenge.example.com", "abd"); c != http.StatusCreated {
		t.Fatalf("second value: %d", c)
	}
}
