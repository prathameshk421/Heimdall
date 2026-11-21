package uidsandbox

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testAllow(t *testing.T) Allowlist {
	t.Helper()
	a, err := NewAllowlist([]string{"proxy.golang.org", "Sum.Golang.Org."}, []int{443})
	require.NoError(t, err)
	return a
}

func TestAllowlistCheck(t *testing.T) {
	a := testAllow(t)
	cases := []struct {
		target string
		want   string
		err    error
	}{
		{"proxy.golang.org:443", "proxy.golang.org:443", nil},
		{"PROXY.golang.org:443", "proxy.golang.org:443", nil},
		{"proxy.golang.org.:443", "proxy.golang.org:443", nil},
		{"sum.golang.org:443", "sum.golang.org:443", nil},
		{"proxy.golang.org..:443", "", ErrBadTarget},
		{"example.com:443", "", ErrHostNotAllowed},
		{"evil.proxy.golang.org:443", "", ErrHostNotAllowed},
		{"proxy.golang.org.evil.com:443", "", ErrHostNotAllowed},
		{"proxy.golang.org:80", "", ErrBadPort},
		{"proxy.golang.org:0443", "", ErrBadTarget},
		{"proxy.golang.org:99999", "", ErrBadTarget},
		{"proxy.golang.org:", "", ErrBadTarget},
		{"proxy.golang.org", "", ErrBadTarget},
		{"1.1.1.1:443", "", ErrIPLiteral},
		{"127.0.0.1:443", "", ErrIPLiteral},
		{"[::1]:443", "", ErrIPLiteral},
		{"[2606:4700::1111]:443", "", ErrIPLiteral},
		{"2130706433:443", "", ErrIPLiteral},
		{"0x7f.1:443", "", ErrIPLiteral},
		{"127.1:443", "", ErrIPLiteral},
		{":443", "", ErrBadTarget},
		{"proxy_golang.org:443", "", ErrBadTarget},
		{"-proxy.golang.org:443", "", ErrBadTarget},
		{"proxy.golang.org\x00.evil:443", "", ErrBadTarget},
		{strings.Repeat("a", 64) + ".org:443", "", ErrBadTarget},
	}
	for _, c := range cases {
		t.Run(c.target, func(t *testing.T) {
			got, err := a.Check(c.target)
			if c.err != nil {
				require.ErrorIs(t, err, c.err)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestNewAllowlistRefusesBadEntries(t *testing.T) {
	_, err := NewAllowlist([]string{"10.0.0.1"}, []int{443})
	require.ErrorIs(t, err, ErrIPLiteral)
	_, err = NewAllowlist([]string{"a..b"}, []int{443})
	require.ErrorIs(t, err, ErrBadTarget)
	_, err = NewAllowlist([]string{"proxy.golang.org"}, []int{0})
	require.ErrorIs(t, err, ErrBadPort)
	_, err = NewAllowlist(nil, []int{443})
	require.ErrorIs(t, err, ErrHostNotAllowed)
}

func TestParseConnect(t *testing.T) {
	cases := []struct {
		line string
		want string
		err  error
	}{
		{"CONNECT proxy.golang.org:443 HTTP/1.1\r\n", "proxy.golang.org:443", nil},
		{"CONNECT proxy.golang.org:443 HTTP/1.0\n", "proxy.golang.org:443", nil},
		{"GET http://proxy.golang.org/ HTTP/1.1\r\n", "", ErrNotConnect},
		{"connect proxy.golang.org:443 HTTP/1.1\r\n", "", ErrNotConnect},
		{"CONNECT proxy.golang.org:443\r\n", "", ErrBadRequestLine},
		{"CONNECT  proxy.golang.org:443 HTTP/1.1\r\n", "", ErrBadRequestLine},
		{"CONNECT proxy.golang.org:443 HTTP/2\r\n", "", ErrBadRequestLine},
		{"CONNECT proxy.golang.org:443 HTTP/1.1 extra\r\n", "", ErrBadRequestLine},
		{"CONNECT user@proxy.golang.org:443 HTTP/1.1\r\n", "", ErrBadTarget},
		{"CONNECT proxy.golang.org:443/x HTTP/1.1\r\n", "", ErrBadTarget},
		{"\r\n", "", ErrBadRequestLine},
	}
	for _, c := range cases {
		t.Run(c.line, func(t *testing.T) {
			got, err := parseConnect(c.line)
			if c.err != nil {
				require.ErrorIs(t, err, c.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestPublicAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"1.1.1.1": true, "142.250.1.1": true, "2606:4700::1111": true,
		"127.0.0.1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.1.1": false,
		"169.254.169.254": false, "100.64.0.1": false, "0.0.0.0": false, "::1": false, "::": false,
		"fe80::1": false, "fc00::1": false, "224.0.0.1": false, "::ffff:127.0.0.1": false,
	} {
		assert.Equal(t, want, publicAddr(netip.MustParseAddr(addr)), addr)
	}
	require.ErrorIs(t, guardControl("tcp4", "169.254.169.254:443", nil), ErrPrivateAddress)
	require.ErrorIs(t, guardControl("tcp4", "not-an-addr", nil), ErrPrivateAddress)
	require.NoError(t, guardControl("tcp4", "1.1.1.1:443", nil))
}

// startTestProxy runs the proxy with a dialer that sends every allowed target to a local echo server, so the test
// needs no network. The allowlist logic and the request parsing are the production code.
func startTestProxy(t *testing.T) (*Proxy, *[]string) {
	t.Helper()
	var lc net.ListenConfig
	echo, err := lc.Listen(context.Background(), "tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = echo.Close() })
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(c, c); _ = c.Close() }()
		}
	}()
	var dialled []string
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		dialled = append(dialled, address)
		var d net.Dialer
		return d.DialContext(ctx, network, echo.Addr().String())
	}
	p, err := startProxy(context.Background(), testAllow(t), slog.New(slog.DiscardHandler), dial)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	return p, &dialled
}

func roundTrip(t *testing.T, p *Proxy, request string) (string, net.Conn, *bufio.Reader) {
	t.Helper()
	var d net.Dialer
	c, err := d.DialContext(context.Background(), "tcp4", p.ln.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = io.WriteString(c, request)
	require.NoError(t, err)
	br := bufio.NewReader(c)
	status, err := br.ReadString('\n')
	require.NoError(t, err)
	return status, c, br
}

func TestProxyTunnelsAllowedHost(t *testing.T) {
	p, dialled := startTestProxy(t)
	status, c, br := roundTrip(t, p, "CONNECT proxy.golang.org.:443 HTTP/1.1\r\nHost: proxy.golang.org:443\r\n\r\nping\n")
	assert.Equal(t, "HTTP/1.1 200 Connection established\r\n", status)
	blank, err := br.ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "\r\n", blank)
	echoed, err := br.ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "ping\n", echoed)
	_ = c.Close()
	assert.Equal(t, []string{"proxy.golang.org:443"}, *dialled)
}

func TestProxyRefusals(t *testing.T) {
	cases := map[string]string{
		"disallowed host":  "CONNECT example.com:443 HTTP/1.1\r\n\r\n",
		"IP literal":       "CONNECT 1.1.1.1:443 HTTP/1.1\r\n\r\n",
		"IPv6 literal":     "CONNECT [::1]:443 HTTP/1.1\r\n\r\n",
		"wrong port":       "CONNECT proxy.golang.org:22 HTTP/1.1\r\n\r\n",
		"bad CONNECT line": "CONNECT proxy.golang.org:443\r\n\r\n",
		"plain GET":        "GET http://proxy.golang.org/ HTTP/1.1\r\nHost: proxy.golang.org\r\n\r\n",
		"long line":        "CONNECT " + strings.Repeat("a", 2000) + ":443 HTTP/1.1\r\n\r\n",
		"huge headers":     "CONNECT proxy.golang.org:443 HTTP/1.1\r\n" + strings.Repeat("X-A: b\r\n", 2000) + "\r\n",
	}
	want := map[string]string{
		"disallowed host": "403", "IP literal": "403", "IPv6 literal": "403", "wrong port": "403",
		"bad CONNECT line": "400", "plain GET": "405", "long line": "400", "huge headers": "400",
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			p, dialled := startTestProxy(t)
			status, _, _ := roundTrip(t, p, req)
			assert.True(t, strings.HasPrefix(status, "HTTP/1.1 "+want[name]+" "), status)
			assert.Empty(t, *dialled, "a refused request must never dial")
		})
	}
}

func TestProxyRefusesPrivateResolution(t *testing.T) {
	d := &net.Dialer{Control: guardControl}
	p, err := startProxy(context.Background(), testAllow(t), slog.New(slog.DiscardHandler),
		func(ctx context.Context, network, _ string) (net.Conn, error) {
			// The allowed name "resolved" to loopback: the guard must refuse it before the connect.
			return d.DialContext(ctx, network, "127.0.0.1:443")
		})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	status, _, _ := roundTrip(t, p, "CONNECT proxy.golang.org:443 HTTP/1.1\r\n\r\n")
	assert.True(t, strings.HasPrefix(status, "HTTP/1.1 403 "), status)
}

func TestProxyCloseEndsTunnels(t *testing.T) {
	p, _ := startTestProxy(t)
	status, c, br := roundTrip(t, p, "CONNECT proxy.golang.org:443 HTTP/1.1\r\n\r\n")
	require.True(t, strings.HasPrefix(status, "HTTP/1.1 200 "))
	_, _ = br.ReadString('\n')
	require.NoError(t, p.Close())
	_, err := br.ReadByte()
	require.Error(t, err)
	_ = c.Close()
}
