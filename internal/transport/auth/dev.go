package auth

import (
	"errors"
	"fmt"
	"net"
	"net/http"
)

// errNotLoopback is the refusal of dev mode on a listener other machines can reach.
var errNotLoopback = errors.New("dev mode (board_auth.mode = off) serves only on a loopback address")

// Off is dev mode: every request is the admin "developer", with no sign-in. It still refuses a cross-origin unsafe
// request. Use it only after CheckLoopback has accepted the listener.
func Off() func(http.Handler) http.Handler {
	origin := http.NewCrossOriginProtection()
	dev := Principal{Person: "developer", Login: "developer", Role: RoleAdmin}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !safeMethod(r.Method) {
				if err := origin.Check(r); err != nil {
					http.Error(w, "cross-origin request refused", http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), dev)))
		})
	}
}

// CheckLoopback refuses a listener that is reachable from another machine. The server calls it on the real
// listener in dev mode, after the config check of the listen text, so a name that resolves elsewhere is caught too.
func CheckLoopback(addr net.Addr) error {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok || tcp == nil {
		return fmt.Errorf("%w: %v is not a TCP address", errNotLoopback, addr)
	}
	if !tcp.IP.IsLoopback() {
		return fmt.Errorf("%w, not %s", errNotLoopback, tcp)
	}
	return nil
}

// safeMethod reports whether a request method only reads.
func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}
