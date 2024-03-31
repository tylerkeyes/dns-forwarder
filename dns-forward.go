package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/miekg/dns"
)

const (
	CLOUDFLARE_DNS = "1.1.1.1:53"
	GOOGLE_DNS     = "8.8.8.8:53"
	absoluteMinTTL = 60
	absoluteMaxTTL = 2147483647
)

var (
	upServer   = flag.String("s", CLOUDFLARE_DNS, fmt.Sprintf("upstream server to connect to. <ip_addr:port>, default %v", CLOUDFLARE_DNS))
	listenAddr = flag.String("a", ":53", "`address:port` to listen on. To listen on the loopback interface only, use `127.0.0.1:53`, to listen on any interface use `:53`.")
	fakeAdd    = flag.String("fakead", "127.0.0.1", "an ip to send back for filtered domains")
	upConn     = flag.String("up-conn", "udp", "upstream dns connection type <udp|tcp>")
	listenConn = flag.String("listen-conn", "udp", "dns server connection type <udp|tcp>")

	flush     = make(chan struct{})
	dataCache = make(map[string]string)
	dataMux   = &sync.Mutex{}
)

func main() {
	flag.Parse()

	server := &dns.Server{Addr: *listenAddr, Net: *listenConn}
	server.Handler = &dnsHandler{}
	log.Printf("upstream dns: %v connection %v", *upServer, *upConn)
	log.Printf("dns listen on: %v connection %v", *listenAddr, *listenConn)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("fatal: failed to set udp|tcp listener %s\n", err.Error())
	}
}

type dnsHandler struct{}

func (*dnsHandler) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	msg := dns.Msg{}
	msg.SetReply(r)

	switch r.Question[0].Qtype {
	case dns.TypeA:
		msg.Authoritative = true
		domain := msg.Question[0].Name
		address, ok := checkDomain(domain)
		if ok {
			msg.Answer = append(msg.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: domain, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
				A:   net.ParseIP(address),
			})
		}
	}
	w.WriteMsg(&msg)
}

func checkDomain(domain string) (string, bool) {
	for k, v := range dataCache {
		if k == domain {
			return v, true
		}
	}

	if addr, ok := resolveDomain(domain); ok {
		dataMux.Lock()
		dataCache[domain] = addr
		dataMux.Unlock()
		return addr, true
	}

	return "err", false
}

func resolveDomain(domain string) (string, bool) {
	req := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network string, address string) (net.Conn, error) {
			dial := net.Dialer{
				Timeout: time.Duration(10000) * time.Millisecond,
			}
			return dial.DialContext(ctx, *upConn, *upServer)
		},
	}

	ip, err := req.LookupHost(context.Background(), domain)
	if err != nil {
		return "err", false
	}
	return ip[0], true
}