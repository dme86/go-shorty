package meta

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"testing"
	"time"
)

func TestMetadataURLValidation(t *testing.T) {
	blocked := []string{
		"file:///etc/passwd", "gopher://example.com/", "http://localhost/",
		"http://foo.local/path", "http://metadata.google.internal/",
		"http://127.0.0.1:80/", "http://[::1]/", "http://[::ffff:127.0.0.1]/",
		"http://169.254.169.254/latest/meta-data/", "http://10.0.0.1/",
		"http://user:pass@example.com/", "http://example.com:8080/",
		"http://example.com:2375/", "http://example.com\\@127.0.0.1/",
	}
	for _, raw := range blocked {
		t.Run(raw, func(t *testing.T) {
			u, err := url.Parse(raw)
			if err == nil {
				err = validateMetadataURL(u)
			}
			if err == nil {
				t.Errorf("metadata fetch unexpectedly allowed %q", raw)
			}
		})
	}
	for _, raw := range []string{"https://example.com/path", "http://example.com:80/", "https://[2606:4700:4700::1111]/"} {
		u, err := url.Parse(raw)
		if err != nil || validateMetadataURL(u) != nil {
			t.Errorf("valid public URL rejected: %s (%v)", raw, err)
		}
	}
}

func TestMetadataIPFiltering(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "10.1.2.3", "192.168.1.1", "172.16.1.1",
		"100.64.0.1", "169.254.169.254", "0.0.0.0",
		"192.0.2.5", "198.18.0.1", "198.51.100.1", "203.0.113.1",
		"224.0.0.1", "255.255.255.255",
		"::1", "::ffff:127.0.0.1", "fe80::1", "fc00::1",
		"2001:db8::1", "2002:0a00:0001::1", "64:ff9b::a9fe:a9fe",
	}
	for _, raw := range blocked {
		if isPublicIP(netip.MustParseAddr(raw)) {
			t.Errorf("blocked IP considered public: %s", raw)
		}
	}
	for _, raw := range []string{"1.1.1.1", "8.8.8.8", "93.184.216.34", "2606:4700:4700::1111"} {
		if !isPublicIP(netip.MustParseAddr(raw)) {
			t.Errorf("public IP incorrectly blocked: %s", raw)
		}
	}
}

func TestMetadataDialPinsAndRejectsDNSResults(t *testing.T) {
	ctx := context.Background()
	const host = "example.com:443"
	dialCount := 0
	var calledAddress string
	dial := func(_ context.Context, _, addr string) (net.Conn, error) {
		dialCount++
		calledAddress = addr
		return nil, errors.New("expected synthetic dial failure")
	}
	lookupPublic := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	if _, err := metadataDialContext(ctx, "tcp", host, lookupPublic, dial); err == nil {
		t.Fatal("fake dial unexpectedly succeeded")
	}
	if dialCount != 1 || calledAddress != "93.184.216.34:443" {
		t.Fatalf("dial was not pinned to vetted IP: %s (calls %d)", calledAddress, dialCount)
	}
	dialCount = 0
	lookupMixed := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("127.0.0.1")}, nil
	}
	if _, err := metadataDialContext(ctx, "tcp", host, lookupMixed, dial); err == nil {
		t.Fatal("mixed public/private DNS response unexpectedly accepted")
	}
	if dialCount != 0 {
		t.Fatal("dial attempted despite private DNS result")
	}
	if _, err := metadataDialContext(ctx, "tcp", "169.254.169.254:80", lookupPublic, dial); err == nil {
		t.Fatal("metadata IP unexpectedly accepted")
	}
	if dialCount != 0 {
		t.Fatal("dial attempted to a prohibited IP")
	}
}

func TestMetadataRedirectValidation(t *testing.T) {
	client := newMetadataClient(time.Second)
	tests := []struct{ target string; allowed bool }{
		{"https://example.com/second", true},
		{"http://127.0.0.1/admin", false},
		{"http://169.254.169.254/latest", false},
		{"http://private.local/", false},
		{"file:///etc/passwd", false},
	}
	for _, tt := range tests {
		u, err := url.Parse(tt.target)
		if err != nil { t.Fatal(err) }
		err = client.CheckRedirect(&http.Request{URL: u}, []*http.Request{{URL: &url.URL{Scheme: "https", Host: "example.com"}}})
		if (err == nil) != tt.allowed {
			t.Errorf("redirect %q: got %v (allowed=%v)", tt.target, err, tt.allowed)
		}
	}
	via := make([]*http.Request, 5)
	if err := client.CheckRedirect(&http.Request{URL: &url.URL{Scheme: "https", Host: "example.com"}}, via); err == nil {
		t.Fatal("redirect limit not enforced")
	}
}
