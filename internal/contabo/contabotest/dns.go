package contabotest

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/miekg/dns"
)

// DNS serves the fake's records over DNS, the way Contabo's name servers
// (ns1/ns2/ns3.contabo.net) serve a real zone. cert-manager's conformance
// suite, cert-manager's propagation check and the Pebble ACME test server
// all read the challenge TXT records from it.
//
// It answers authoritatively for every zone in the fake: SOA and NS at the
// zone apex, TXT, A, AAAA, CNAME and MX anywhere. A name with no records
// is NXDOMAIN, unless records exist below it (an "empty non-terminal").
// TXT data is served like Contabo does: surrounding quotes are removed, so
// abc and "abc" both come out as abc, and values longer than 255 bytes are
// split into several strings.
//
// Names outside every zone also get NXDOMAIN. A real authoritative server
// would answer REFUSED, but test clients that walk up the tree (CAA
// lookups, SOA searches) cope better with NXDOMAIN.
type DNS struct {
	fake *Server
	udp  *dns.Server
	tcp  *dns.Server
	addr string
}

// StartDNS serves DNS on addr (for example "127.0.0.1:0") over UDP and TCP
// and returns once both listeners are ready. With port 0, both share one
// random port. Close it when done.
func (s *Server) StartDNS(addr string) (*DNS, error) {
	h := dns.HandlerFunc(s.ServeDNS)
	var lc net.ListenConfig
	pc, err := lc.ListenPacket(context.Background(), "udp", addr)
	if err != nil {
		return nil, err
	}
	// Use the same port for TCP as UDP got.
	l, err := lc.Listen(context.Background(), "tcp", pc.LocalAddr().String())
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	d := &DNS{fake: s, addr: pc.LocalAddr().String()}
	var wg sync.WaitGroup
	wg.Add(2)
	d.udp = &dns.Server{PacketConn: pc, Handler: h, NotifyStartedFunc: wg.Done}
	d.tcp = &dns.Server{Listener: l, Handler: h, NotifyStartedFunc: wg.Done}
	go func() { _ = d.udp.ActivateAndServe() }()
	go func() { _ = d.tcp.ActivateAndServe() }()
	wg.Wait()
	return d, nil
}

// Addr is the host:port the DNS server listens on.
func (d *DNS) Addr() string { return d.addr }

// Close stops the DNS server.
func (d *DNS) Close() {
	_ = d.udp.Shutdown()
	_ = d.tcp.Shutdown()
}

// ServeDNS answers one DNS query from the fake's records.
func (s *Server) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(req)
	m.Authoritative = true
	m.RecursionAvailable = false
	if req.Opcode != dns.OpcodeQuery || len(req.Question) != 1 {
		m.SetRcode(req, dns.RcodeNotImplemented)
		_ = w.WriteMsg(m)
		return
	}
	s.count("dns")
	q := req.Question[0]
	name := strings.ToLower(strings.TrimSuffix(q.Name, "."))

	s.mu.Lock()
	zone := s.zoneOfLocked(name)
	var recs []Record
	exists := false
	var soa *Record
	if zone != "" {
		for _, r := range s.zones[zone] {
			n := fqdn(r.Name, zone)
			if n == name {
				exists = true
				recs = append(recs, *r)
			} else if strings.HasSuffix(n, "."+name) {
				exists = true // empty non-terminal
			}
			if r.Type == "SOA" && soa == nil {
				c := *r
				soa = &c
			}
		}
	}
	s.mu.Unlock()

	if zone == "" {
		m.SetRcode(req, dns.RcodeNameError)
		_ = w.WriteMsg(m)
		return
	}

	hasCNAME := false
	for _, r := range recs {
		if r.Type == "CNAME" {
			hasCNAME = true
		}
	}
	for _, r := range recs {
		match := r.Type == dns.TypeToString[q.Qtype] || q.Qtype == dns.TypeANY ||
			(hasCNAME && r.Type == "CNAME" && q.Qtype != dns.TypeCNAME)
		if !match {
			continue
		}
		if rr := toRR(q.Name, r); rr != nil {
			m.Answer = append(m.Answer, rr)
		}
	}
	if len(m.Answer) == 0 {
		if !exists {
			m.Rcode = dns.RcodeNameError
		}
		if soa != nil {
			if rr := toRR(dns.Fqdn(zone), *soa); rr != nil {
				m.Ns = append(m.Ns, rr)
			}
		}
	}
	_ = w.WriteMsg(m)
}

// zoneOfLocked returns the longest zone that contains name. s.mu must be held.
func (s *Server) zoneOfLocked(name string) string {
	best := ""
	for z := range s.zones {
		if (name == z || strings.HasSuffix(name, "."+z)) && len(z) > len(best) {
			best = z
		}
	}
	return best
}

func toRR(owner string, r Record) dns.RR {
	ttl := uint32(300)
	if r.TTL != nil && *r.TTL >= 0 {
		ttl = uint32(*r.TTL)
	}
	hdr := func(t uint16) dns.RR_Header {
		return dns.RR_Header{Name: dns.Fqdn(owner), Rrtype: t, Class: dns.ClassINET, Ttl: ttl}
	}
	data := strings.TrimSpace(r.Data)
	switch r.Type {
	case "TXT":
		return &dns.TXT{Hdr: hdr(dns.TypeTXT), Txt: TXTStrings(data)}
	case "A":
		if ip := net.ParseIP(data).To4(); ip != nil {
			return &dns.A{Hdr: hdr(dns.TypeA), A: ip}
		}
	case "AAAA":
		if ip := net.ParseIP(data); ip != nil {
			return &dns.AAAA{Hdr: hdr(dns.TypeAAAA), AAAA: ip}
		}
	case "CNAME":
		return &dns.CNAME{Hdr: hdr(dns.TypeCNAME), Target: dns.Fqdn(data)}
	case "NS":
		return &dns.NS{Hdr: hdr(dns.TypeNS), Ns: dns.Fqdn(data)}
	case "MX":
		prio := uint16(0)
		if r.Prio != nil {
			prio = uint16(*r.Prio)
		}
		return &dns.MX{Hdr: hdr(dns.TypeMX), Preference: prio, Mx: dns.Fqdn(data)}
	case "SOA":
		f := strings.Fields(data)
		if len(f) != 7 {
			return nil
		}
		n := make([]uint32, 5)
		for i := range 5 {
			v, err := strconv.ParseUint(f[i+2], 10, 32)
			if err != nil {
				return nil
			}
			n[i] = uint32(v)
		}
		return &dns.SOA{Hdr: hdr(dns.TypeSOA), Ns: dns.Fqdn(f[0]), Mbox: dns.Fqdn(f[1]),
			Serial: n[0], Refresh: n[1], Retry: n[2], Expire: n[3], Minttl: n[4]}
	}
	return nil
}

// TXTStrings turns stored TXT data into the strings a name server sends:
// one pair of surrounding quotes is removed and the rest is split into
// chunks of at most 255 bytes.
func TXTStrings(data string) []string {
	if len(data) >= 2 && strings.HasPrefix(data, `"`) && strings.HasSuffix(data, `"`) {
		data = data[1 : len(data)-1]
	}
	var out []string
	for len(data) > 255 {
		out = append(out, data[:255])
		data = data[255:]
	}
	return append(out, data)
}

// TXTValues returns the values of the TXT records at name as a name server
// would serve them (quotes removed, chunks joined). Tests use it to check
// what is "in DNS".
func (s *Server) TXTValues(zone, name string) []string {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	var out []string
	for _, r := range s.Records(zone) {
		if r.Type == "TXT" && fqdn(r.Name, zone) == name {
			out = append(out, strings.Join(TXTStrings(strings.TrimSpace(r.Data)), ""))
		}
	}
	return out
}
