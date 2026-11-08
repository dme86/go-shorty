package meta

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var blockedIPRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),   // NAT64 can embed private IPv4 destinations
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001::/23"),     // protocol assignments / Teredo
	netip.MustParsePrefix("2001:db8::/32"), // documentation
	netip.MustParsePrefix("2002::/16"),     // 6to4 can embed private IPv4
	netip.MustParsePrefix("3fff::/20"),     // documentation
}

// validateMetadataURL only permits normal public HTTP(S) destinations. This
// check is performed for the initial URL and every redirect; actual DNS
// addresses are checked separately at connection time.
func validateMetadataURL(u *url.URL) error {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Opaque != "" || u.User != nil {
		return errors.New("metadata URL must be http(s) without credentials")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" || strings.ContainsAny(host, "%\\") {
		return errors.New("invalid metadata hostname")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		host == "local" || strings.HasSuffix(host, ".local") ||
		host == "internal" || strings.HasSuffix(host, ".internal") ||
		host == "home.arpa" || strings.HasSuffix(host, ".home.arpa") {
		return errors.New("metadata hostname is not public")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || (n != 80 && n != 443) {
			return errors.New("metadata fetch only supports ports 80 and 443")
		}
	}
	if ip, err := netip.ParseAddr(host); err == nil && !isPublicIP(ip) {
		return errors.New("metadata IP is not public")
	}
	return nil
}

func isPublicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	for _, prefix := range blockedIPRanges {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

// metadataDialContext resolves the hostname once, rejects the entire set
// if ANY DNS result is non-public, and dials ONLY the vetted numeric address.
// That avoids the DNS rebinding TOCTOU vulnerability of validating a hostname
// and subsequently resolving it again in the HTTP transport.
func metadataDialContext(
	ctx context.Context, network, address string,
	lookup func(context.Context, string, string) ([]netip.Addr, error),
	dial func(context.Context, string, string) (net.Conn, error),
) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid metadata address: %w", err)
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return nil, errors.New("empty metadata hostname")
	}
	var ips []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{ip}
	} else {
		if host == "localhost" || strings.HasSuffix(host, ".localhost") ||
			host == "local" || strings.HasSuffix(host, ".local") ||
			host == "internal" || strings.HasSuffix(host, ".internal") ||
			host == "home.arpa" || strings.HasSuffix(host, ".home.arpa") {
			return nil, errors.New("local metadata hostname is prohibited")
		}
		ips, err = lookup(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve metadata hostname: %w", err)
		}
	}
	if len(ips) == 0 {
		return nil, errors.New("metadata hostname has no addresses")
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return nil, fmt.Errorf("metadata DNS result is not public: %s", ip)
		}
	}
	var lastErr error
	for _, ip := range ips {
		// The dialer never receives a hostname: it cannot resolve to an
		// unchecked private address between validation and connection.
		conn, err := dial(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("connect to public metadata host: %w", lastErr)
}

func newMetadataClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	transport := &http.Transport{
		Proxy:                  nil, // never use HTTP_PROXY to bypass IP validation
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return metadataDialContext(ctx, network, addr, net.DefaultResolver.LookupNetIP, dialer.DialContext)
		},
		TLSHandshakeTimeout:    3 * time.Second,
		ResponseHeaderTimeout: 3 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
		DisableKeepAlives:      true,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many metadata redirects")
			}
			return validateMetadataURL(req.URL)
		},
	}
}
