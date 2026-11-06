package rootlessrunner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const maxFetchedArtifact = 64 << 20

// Fetcher fetches approved artifacts without executing them. Every request and
// redirect is restricted to the profile's exact host list, and private IPs are
// refused after DNS resolution to prevent metadata and local-network access.
type Fetcher struct {
}

// Fetch downloads a bounded artifact from an exact host in the approved profile.
func (f Fetcher) Fetch(ctx context.Context, hosts []string, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || !allowedFetchURL(hosts, u) {
		return nil, errors.New("fetch URL host is not allowlisted")
	}
	client := f.client(hosts)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create fetch request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch approved artifact: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch approved artifact: status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchedArtifact+1))
	if err != nil {
		return nil, fmt.Errorf("read approved artifact: %w", err)
	}
	if len(data) > maxFetchedArtifact {
		return nil, errors.New("fetched artifact exceeds 64 MiB")
	}
	return data, nil
}

func (f Fetcher) client(hosts []string) *http.Client {
	transport := &http.Transport{DialContext: safeDial(hosts), TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, IdleConnTimeout: 30 * time.Second}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if !allowedFetchURL(hosts, req.URL) {
			return errors.New("redirect target host is not allowlisted")
		}
		return nil
	}
	return client
}

func allowedFetchURL(hosts []string, u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Host != u.Hostname() {
		return false
	}
	return slicesContains(hosts, strings.ToLower(u.Hostname()))
}

func safeDial(hosts []string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, _, address string) (net.Conn, error) {
		return dialAllowlisted(ctx, hosts, address)
	}
}

func dialAllowlisted(ctx context.Context, hosts []string, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || !slicesContains(hosts, strings.ToLower(host)) || port != "443" {
		return nil, errors.New("fetch connection target is not allowlisted")
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve allowlisted fetch host: %w", err)
	}
	public := publicAddresses(ips)
	if len(public) == 0 {
		return nil, errors.New("fetch host resolved to no public addresses")
	}
	return dialPublic(ctx, public, port)
}

func publicAddresses(ips []netip.Addr) []netip.Addr {
	var public []netip.Addr
	for _, ip := range ips {
		if ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified() {
			public = append(public, ip)
		}
	}
	return public
}

func dialPublic(ctx context.Context, ips []netip.Addr, port string) (net.Conn, error) {
	dialer := net.Dialer{Timeout: 10 * time.Second}
	var lastErr error
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("dial allowlisted fetch host: %w", lastErr)
}

func slicesContains(values []string, value string) bool {
	for _, item := range values {
		if strings.EqualFold(item, value) {
			return true
		}
	}
	return false
}
