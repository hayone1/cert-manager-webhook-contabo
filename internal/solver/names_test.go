package solver

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestNormalizeName(t *testing.T) {
	for _, c := range [][2]string{
		{"", ""},
		{".", ""},
		{"	Example.COM. ", "example.com"},
		{"_acme-challenge.Example.com.", "_acme-challenge.example.com"},
		{"_acme-challenge.bücher.example", "_acme-challenge.xn--bcher-kva.example"},
		{"xn--bcher-kva.example", "xn--bcher-kva.example"},
		{"*.example.com", "*.example.com"},
	} {
		in, want := c[0], c[1]
		got, err := normalizeName(in)
		if err != nil || got != want {
			t.Errorf("normalizeName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"a..b", ".a", strings.Repeat("a.", 130) + "com"} {
		if _, err := normalizeName(bad); err == nil {
			t.Errorf("normalizeName(%q) should fail", bad)
		}
	}
}

func TestRecordFQDN(t *testing.T) {
	const zone = "example.com"
	for in, want := range map[string]string{
		"":                             zone,
		"@":                            zone,
		"example.com":                  zone,
		"EXAMPLE.com.":                 zone,
		"_acme-challenge":              "_acme-challenge.example.com",
		"_acme-challenge.example.com":  "_acme-challenge.example.com",
		"_acme-challenge.example.com.": "_acme-challenge.example.com",
		"_acme-challenge.www":          "_acme-challenge.www.example.com",
		"a..b":                         "\x00invalid.example.com",
	} {
		if got := recordFQDN(in, zone); got != want {
			t.Errorf("recordFQDN(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestZonesContaining(t *testing.T) {
	zones := []string{"example.com", "sub.example.com", "other.org", "ample.com", "example.com"}
	got := zonesContaining("_acme-challenge.www.sub.example.com", zones)
	if !slices.Equal(got, []string{"sub.example.com", "example.com"}) {
		t.Fatalf("got %v", got)
	}
	if got := zonesContaining("_acme-challenge.notexample.com", zones); len(got) != 0 {
		t.Fatalf("suffix match must be on a label boundary, got %v", got)
	}
}

func TestPickZone(t *testing.T) {
	zones := []string{"example.com", "sub.example.com", "other.org"}
	cases := []struct {
		fqdn, resolved, forced string
		want, err              string
	}{
		{"_acme-challenge.example.com", "example.com", "", "example.com", ""},
		{"_acme-challenge.www.sub.example.com", "sub.example.com", "", "sub.example.com", ""},
		{"_acme-challenge.www.sub.example.com", "", "", "sub.example.com", ""},
		// The account holds the parent too, and DNS says the parent is the zone.
		{"_acme-challenge.www.sub.example.com", "example.com", "", "example.com", ""},
		{"_acme-challenge.deep.other.org", "deep.other.org", "", "", "public DNS says"},
		{"_acme-challenge.deep.other.org", "deep.other.org", "other.org", "other.org", ""},
		{"_acme-challenge.missing.net", "missing.net", "", "", "no zone that contains"},
		{"_acme-challenge.example.com", "", "missing.net", "", "not in this Contabo account"},
		{"_acme-challenge.example.com", "", "other.org", "", "does not contain"},
		{"_acme-challenge.example.com", "com", "", "", "public DNS says"},
	}
	for _, c := range cases {
		got, err := pickZone(c.fqdn, c.resolved, c.forced, zones)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("pickZone(%q, %q, %q) error = %v, want %q", c.fqdn, c.resolved, c.forced, err, c.err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("pickZone(%q, %q, %q) = %q, %v; want %q", c.fqdn, c.resolved, c.forced, got, err, c.want)
		}
	}
	_, err := pickZone("_acme-challenge.x.net", "", "", nil)
	if !errors.Is(err, errNoZone) || !strings.Contains(err.Error(), "zones in the account: none") {
		t.Fatalf("got %v", err)
	}
}

func TestZoneListIsShortened(t *testing.T) {
	var zones []string
	for i := range 15 {
		zones = append(zones, string(rune('a'+i))+".com")
	}
	if got := zoneList(zones); !strings.HasSuffix(got, "and 5 more") {
		t.Fatalf("got %q", got)
	}
}

func TestTXTValue(t *testing.T) {
	for in, want := range map[string]string{
		`abc`:                   "abc",
		` abc `:                 "abc",
		`"abc"`:                 "abc",
		`"ab" "c"`:              "abc",
		`"ab"   "c"`:            "abc",
		`"a\"b"`:                `a"b`,
		`"a\\b"`:                `a\b`,
		`"unterminated`:         `"unterminated`,
		`"a" junk`:              `"a" junk`,
		`""`:                    "",
		`"123d=="`:              "123d==",
		`v=spf1 include:x -all`: "v=spf1 include:x -all",
	} {
		if got := txtValue(in); got != want {
			t.Errorf("txtValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseConfigDefaults(t *testing.T) {
	cfg, err := parseConfig(nil)
	if err != nil || cfg.SecretName != "" || cfg.ttl() != DefaultTTL {
		t.Fatalf("nil config: %+v %v", cfg, err)
	}
	k := SecretKeys{APIUser: "user"}.withDefaults()
	if k.ClientID != DefaultClientIDKey || k.APIUser != "user" || k.APIPassword != DefaultAPIPasswordKey {
		t.Fatalf("defaults: %+v", k)
	}
}
