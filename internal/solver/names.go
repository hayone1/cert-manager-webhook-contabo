package solver

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

// errNoZone means the Contabo account has no zone that contains the name.
var errNoZone = errors.New("no matching Contabo zone")

// normalizeName turns a DNS name into the form used for comparisons:
// lower case, no surrounding spaces, no trailing dot, and international
// labels in their ASCII ("xn--") form.
//
// Labels that are already ASCII are only lower-cased. They are not run
// through IDNA, because IDNA refuses the underscore in "_acme-challenge".
func normalizeName(name string) (string, error) {
	name = strings.TrimSuffix(strings.TrimSpace(name), ".")
	if name == "" {
		return "", nil
	}
	labels := strings.Split(name, ".")
	for i, l := range labels {
		if l == "" {
			return "", fmt.Errorf("%q has an empty label", name)
		}
		if isASCII(l) {
			labels[i] = strings.ToLower(l)
			continue
		}
		a, err := idna.Lookup.ToASCII(l)
		if err != nil {
			return "", fmt.Errorf("%q is not a valid domain name: %w", name, err)
		}
		labels[i] = a
	}
	out := strings.Join(labels, ".")
	if len(out) > 253 {
		return "", fmt.Errorf("%q is longer than 253 characters", name)
	}
	return out, nil
}

// mustNormalize is normalizeName for names that came from the Contabo API.
// A name the API sends back that cannot be parsed never matches anything.
func mustNormalize(name string) string {
	n, err := normalizeName(name)
	if err != nil {
		return "\x00invalid"
	}
	return n
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// recordFQDN works out the full name of a record as the Contabo API stores
// it. The API keeps the name exactly as it was sent, and these spellings
// all mean the same DNS name:
//
//	"_acme-challenge"                  relative to the zone
//	"_acme-challenge.example.com"      full name
//	"_acme-challenge.example.com."     full name with a trailing dot
//
// The zone apex can be "", "@" or the zone name. Contabo decides that a
// name is already full when it ends with the zone name, so this function
// does the same. zone must be normalized.
func recordFQDN(recordName, zone string) string {
	name := mustNormalize(recordName)
	switch {
	case name == "" || name == "@" || name == zone:
		return zone
	case strings.HasSuffix(name, "."+zone):
		return name
	default:
		return name + "." + zone
	}
}

// inZone reports whether name is the zone itself or below it. Both must be
// normalized.
func inZone(name, zone string) bool {
	return zone != "" && (name == zone || strings.HasSuffix(name, "."+zone))
}

// zonesContaining returns the zones that contain name, longest first, so
// that a delegated child zone comes before its parent.
func zonesContaining(name string, zones []string) []string {
	var out []string
	for _, z := range zones {
		if inZone(name, z) && !slices.Contains(out, z) {
			out = append(out, z)
		}
	}
	slices.SortFunc(out, func(a, b string) int { return len(b) - len(a) })
	return out
}

// pickZone chooses the Contabo zone that should hold the TXT record for
// fqdn. All names must be normalized.
//
//   - forced (the "zone" setting) wins if it is in the account and contains fqdn.
//   - Otherwise the zone cert-manager found by SOA lookup (resolvedZone) is
//     used when the account has it.
//   - Otherwise, with no resolvedZone, the longest matching zone is used.
//
// If the account only has a different zone than resolvedZone, the record
// would sit in a zone that public DNS does not ask for that name, so the
// challenge could never pass. That is an error with an explanation instead
// of a silent wait.
func pickZone(fqdn, resolvedZone, forced string, zones []string) (string, error) {
	if forced != "" {
		if !slices.Contains(zones, forced) {
			return "", fmt.Errorf("%w: the zone %q from the webhook config is not in this Contabo account", errNoZone, forced)
		}
		if !inZone(fqdn, forced) {
			return "", fmt.Errorf("the zone %q from the webhook config does not contain %q", forced, fqdn)
		}
		return forced, nil
	}
	candidates := zonesContaining(fqdn, zones)
	if len(candidates) == 0 {
		return "", fmt.Errorf("%w: this Contabo account has no zone that contains %q (zones in the account: %s)",
			errNoZone, fqdn, zoneList(zones))
	}
	if resolvedZone == "" || slices.Contains(candidates, resolvedZone) {
		if resolvedZone != "" {
			return resolvedZone, nil
		}
		return candidates[0], nil
	}
	return "", fmt.Errorf("public DNS says %q belongs to the zone %q, but this Contabo account only has %q. "+
		"A record in %q would never be seen, so the challenge could not pass. "+
		"Check that the domain's name servers point to Contabo (ns1.contabo.net, ns2.contabo.net, ns3.contabo.net), "+
		"or set \"zone\" in the webhook config if you are sure",
		fqdn, resolvedZone, candidates[0], candidates[0])
}

func zoneList(zones []string) string {
	if len(zones) == 0 {
		return "none"
	}
	const limit = 10
	shown := zones
	if len(shown) > limit {
		shown = shown[:limit]
	}
	s := strings.Join(shown, ", ")
	if len(zones) > limit {
		s += fmt.Sprintf(" and %d more", len(zones)-limit)
	}
	return s
}

// txtValue returns the text a TXT record holds, the way a name server would
// serve it.
//
// The Contabo API stores TXT data exactly as sent, so the same value can
// come back as abc, "abc" or "ab" "c" (long values are split into several
// quoted strings). Quoted strings are joined and their escapes removed.
func txtValue(data string) string {
	s := strings.TrimSpace(data)
	if !strings.HasPrefix(s, `"`) {
		return s
	}
	var b strings.Builder
	for len(s) > 0 {
		if s[0] != '"' {
			// Not a well-formed list of quoted strings: use it as is.
			return strings.TrimSpace(data)
		}
		s = s[1:]
		closed := false
		for len(s) > 0 {
			c := s[0]
			s = s[1:]
			if c == '\\' && len(s) > 0 {
				b.WriteByte(s[0])
				s = s[1:]
				continue
			}
			if c == '"' {
				closed = true
				break
			}
			b.WriteByte(c)
		}
		if !closed {
			return strings.TrimSpace(data)
		}
		s = strings.TrimLeft(s, " \t")
	}
	return b.String()
}
