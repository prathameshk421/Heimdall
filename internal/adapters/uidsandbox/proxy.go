package uidsandbox

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// Errors of the fetch proxy. Each one is a refused request; the client gets a status line and the connection closes.
var (
	ErrNotConnect     = errors.New("only CONNECT is allowed")
	ErrBadRequestLine = errors.New("bad request line")
	ErrBadTarget      = errors.New("bad CONNECT target")
	ErrIPLiteral      = errors.New("IP literal target refused")
	ErrBadPort        = errors.New("port not allowed")
	ErrHostNotAllowed = errors.New("host not in the registry allowlist")
	ErrPrivateAddress = errors.New("host resolves to a non-public address")
)

// Limits of one proxy request head.
const (
	maxRequestLine = 1024
	maxHeaderBytes = 8 << 10
	maxHostLen     = 253
)

// Allowlist holds the registry hosts a fetch phase may reach (the profile's registries, TECH_SPEC 12.2). Hosts match
// exactly after normalisation: lower case, one trailing dot removed. There are no wildcards.
type Allowlist struct {
	hosts map[string]bool
	ports map[int]bool
}

// NewAllowlist builds an allowlist from host names and ports. A host that is not a valid DNS name, or is an IP
// literal, is refused so a bad profile fails at setup and not at the first request.
func NewAllowlist(hosts []string, ports []int) (Allowlist, error) {
	a := Allowlist{hosts: map[string]bool{}, ports: map[int]bool{}}
	for _, h := range hosts {
		n, err := normaliseHost(h)
		if err != nil {
			return Allowlist{}, fmt.Errorf("registry %q: %w", h, err)
		}
		a.hosts[n] = true
	}
	for _, p := range ports {
		if p < 1 || p > 65535 {
			return Allowlist{}, fmt.Errorf("port %d: %w", p, ErrBadPort)
		}
		a.ports[p] = true
	}
	if len(a.hosts) == 0 || len(a.ports) == 0 {
		return Allowlist{}, fmt.Errorf("allowlist needs at least one host and one port: %w", ErrHostNotAllowed)
	}
	return a, nil
}

// Check returns the normalised "host:port" to dial when target is allowed, or the reason it is not.
func (a Allowlist) Check(target string) (string, error) {
	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		return "", fmt.Errorf("%q: %w", target, ErrBadTarget)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != portText {
		return "", fmt.Errorf("%q: %w", target, ErrBadTarget)
	}
	n, err := normaliseHost(host)
	if err != nil {
		return "", fmt.Errorf("%q: %w", target, err)
	}
	if !a.ports[port] {
		return "", fmt.Errorf("%q: %w", target, ErrBadPort)
	}
	if !a.hosts[n] {
		return "", fmt.Errorf("%q: %w", target, ErrHostNotAllowed)
	}
	return net.JoinHostPort(n, portText), nil
}

// normaliseHost lower-cases a DNS name and strips one trailing dot. It refuses IP literals, empty labels, labels over
// 63 bytes and any byte outside letters, digits and hyphens.
func normaliseHost(h string) (string, error) {
	h = strings.ToLower(strings.TrimSuffix(h, "."))
	if strings.HasPrefix(h, "[") || strings.Contains(h, ":") {
		return "", ErrIPLiteral
	}
	if _, err := netip.ParseAddr(h); err == nil {
		return "", ErrIPLiteral
	}
	if h == "" || len(h) > maxHostLen {
		return "", ErrBadTarget
	}
	labels := strings.Split(h, ".")
	for _, l := range labels {
		if !validLabel(l) {
			return "", ErrBadTarget
		}
	}
	// A name whose last label is all digits is an IPv4 form such as 0x7f.1 or 2130706433 that some resolvers accept.
	if allDigits(labels[len(labels)-1]) {
		return "", ErrIPLiteral
	}
	return h, nil
}

func validLabel(l string) bool {
	if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
		return false
	}
	for i := 0; i < len(l); i++ {
		c := l[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// parseConnect reads one request line of the form "CONNECT host:port HTTP/1.x" and returns the target.
func parseConnect(line string) (string, error) {
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	parts := strings.Split(line, " ")
	if len(parts) != 3 {
		return "", ErrBadRequestLine
	}
	method, target, proto := parts[0], parts[1], parts[2]
	if proto != "HTTP/1.1" && proto != "HTTP/1.0" {
		return "", ErrBadRequestLine
	}
	if method != "CONNECT" {
		return "", ErrNotConnect
	}
	if target == "" || strings.ContainsAny(target, "/?#@\t") {
		return "", ErrBadTarget
	}
	return target, nil
}

// publicAddr reports whether a dialled address may be reached from the fetch phase. Loopback, private, link-local
// (169.254.0.0/16 holds cloud metadata), multicast, unspecified and shared-address ranges are refused, so a DNS answer
// that points an allowed name at the runner itself fails closed.
func publicAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	return !netip.MustParsePrefix("100.64.0.0/10").Contains(ip)
}

// guardControl is a net.Dialer Control function that refuses a non-public address after DNS resolution.
func guardControl(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("dial %q: %w", address, ErrPrivateAddress)
	}
	if !publicAddr(ap.Addr()) {
		return fmt.Errorf("dial %s: %w", ap.Addr(), ErrPrivateAddress)
	}
	return nil
}

type dialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// Proxy is the fetch phase's only way out: an HTTP CONNECT proxy on loopback that tunnels to allowlisted registries.
type Proxy struct {
	allow Allowlist
	log   *slog.Logger
	dial  dialFunc
	ln    net.Listener

	mu     sync.Mutex
	conns  map[net.Conn]bool
	closed bool
	wg     sync.WaitGroup
}

// StartProxy listens on 127.0.0.1 on a free port and serves until Close.
func StartProxy(ctx context.Context, allow Allowlist, log *slog.Logger) (*Proxy, error) {
	d := &net.Dialer{Control: guardControl}
	return startProxy(ctx, allow, log, d.DialContext)
}

func startProxy(ctx context.Context, allow Allowlist, log *slog.Logger, dial dialFunc) (*Proxy, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("proxy listen: %w", err)
	}
	p := &Proxy{allow: allow, log: log, dial: dial, ln: ln, conns: map[net.Conn]bool{}}
	p.wg.Add(1)
	go p.serve(context.WithoutCancel(ctx))
	return p, nil
}

// Port is the loopback port the proxy listens on.
func (p *Proxy) Port() int {
	addr, ok := p.ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0
	}
	return addr.Port
}

// Close stops the listener and closes every open tunnel.
func (p *Proxy) Close() error {
	p.mu.Lock()
	p.closed = true
	for c := range p.conns {
		_ = c.Close()
	}
	p.mu.Unlock()
	err := p.ln.Close()
	p.wg.Wait()
	if err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("proxy close: %w", err)
	}
	return nil
}

func (p *Proxy) track(c net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		_ = c.Close()
		return false
	}
	p.conns[c] = true
	return true
}

func (p *Proxy) untrack(c net.Conn) {
	p.mu.Lock()
	delete(p.conns, c)
	p.mu.Unlock()
	_ = c.Close()
}

func (p *Proxy) serve(ctx context.Context) {
	defer p.wg.Done()
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		if !p.track(c) {
			return
		}
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			defer p.untrack(c)
			p.handle(ctx, c)
		}()
	}
}

// readHead reads the request line and the headers, bounded, and returns the request line.
func readHead(r *bufio.Reader) (string, error) {
	line, err := readLine(r, maxRequestLine)
	if err != nil {
		return "", err
	}
	total := len(line)
	for {
		h, err := readLine(r, maxHeaderBytes)
		if err != nil {
			return "", err
		}
		total += len(h)
		if total > maxHeaderBytes {
			return "", ErrBadRequestLine
		}
		if h == "\r\n" || h == "\n" {
			return line, nil
		}
	}
}

func readLine(r *bufio.Reader, limit int) (string, error) {
	var b strings.Builder
	for b.Len() <= limit {
		c, err := r.ReadByte()
		if err != nil {
			return "", fmt.Errorf("read request: %w", err)
		}
		b.WriteByte(c)
		if c == '\n' {
			return b.String(), nil
		}
	}
	return "", ErrBadRequestLine
}

func statusFor(err error) string {
	switch {
	case errors.Is(err, ErrNotConnect):
		return "405 Method Not Allowed"
	case errors.Is(err, ErrBadRequestLine), errors.Is(err, ErrBadTarget):
		return "400 Bad Request"
	case errors.Is(err, ErrPrivateAddress):
		return "403 Forbidden"
	case errors.Is(err, ErrIPLiteral), errors.Is(err, ErrBadPort), errors.Is(err, ErrHostNotAllowed):
		return "403 Forbidden"
	default:
		return "502 Bad Gateway"
	}
}

func (p *Proxy) refuse(ctx context.Context, c net.Conn, target string, err error) {
	p.log.InfoContext(ctx, "sandbox proxy refused", slog.String("target", target), slog.String("reason", err.Error()))
	_, _ = io.WriteString(c, "HTTP/1.1 "+statusFor(err)+"\r\nConnection: close\r\nContent-Length: 0\r\n\r\n")
}

func (p *Proxy) handle(ctx context.Context, c net.Conn) {
	br := bufio.NewReaderSize(c, 4096)
	line, err := readHead(br)
	if err != nil {
		p.refuse(ctx, c, "", fmt.Errorf("%w: %w", ErrBadRequestLine, err))
		return
	}
	target, err := parseConnect(line)
	if err != nil {
		p.refuse(ctx, c, "", err)
		return
	}
	dest, err := p.allow.Check(target)
	if err != nil {
		p.refuse(ctx, c, target, err)
		return
	}
	up, err := p.dial(ctx, "tcp", dest)
	if err != nil {
		p.refuse(ctx, c, target, err)
		return
	}
	if !p.track(up) {
		return
	}
	defer p.untrack(up)
	p.log.InfoContext(ctx, "sandbox proxy tunnel", slog.String("target", dest))
	if _, err := io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() {
		// Bytes the client sent after the head are already in br.
		_, _ = io.Copy(up, br)
		closeWrite(up)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(c, up)
		closeWrite(c)
		done <- struct{}{}
	}()
	<-done
	<-done
}

func closeWrite(c net.Conn) {
	if tc, ok := c.(interface{ CloseWrite() error }); ok {
		_ = tc.CloseWrite()
	}
}
