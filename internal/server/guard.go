package server

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// guard closes the gateway to the user's own browser when no api_keys are
// configured, which is the shipped loopback default. Two paths otherwise
// let a web page the user merely visits drive the gateway:
//
//   - a cross-site request, which the browser labels with an Origin header
//     naming the page's site. A no-preflight POST (text/plain body) still
//     parses as JSON downstream, so the check has to come before handlers;
//   - DNS rebinding, where the page's hostname is pointed at 127.0.0.1 so
//     its requests are same-origin yet arrive with a foreign Host header.
//
// Once api_keys are set the bearer token already defeats both (a page can
// neither read the key nor attach it cross-site without a preflight the
// gateway never answers), so the checks step aside and deployments behind a
// reverse proxy keep working with whatever Host and Origin the proxy sends.
//
// Browser extensions get no blanket pass. Any installed extension, with no
// host permission at all, can send the same no-preflight POST a web page
// can, so trusting every chrome-extension:// origin would leave the shell
// tool one malicious add-on away. An extension either presents an API key
// or has its exact origin listed in server.allowed_origins.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.cfg.Server.APIKeys) == 0 {
			origin := r.Header.Get("Origin")
			if origin != "" && !s.originAllowed(origin, r.Host) {
				writeErr(w, http.StatusForbidden,
					"cross-origin requests are not accepted by a keyless gateway: set server.api_keys and give the client a key, or list this origin under server.allowed_origins", "forbidden")
				return
			}
			// Belt and braces for a state-changing request that arrives
			// without an Origin header: browsers still label where it came
			// from. Reads are left alone so links into the UI keep working.
			if origin == "" && !safeMethod(r.Method) {
				if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "same-site" {
					writeErr(w, http.StatusForbidden,
						"cross-site requests are not accepted by a keyless gateway", "forbidden")
					return
				}
			}
			if !s.hostAllowed(r.Host) {
				writeErr(w, http.StatusForbidden,
					"request Host "+r.Host+" is not an address this gateway serves", "forbidden")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// originAllowed reports whether a request carrying Origin may proceed: the
// gateway's own origin (the served UI), or one the operator listed in
// server.allowed_origins. An opaque "null" origin, which sandboxed frames
// and some redirects produce, matches neither and is refused.
func (s *Server) originAllowed(origin, host string) bool {
	trimmed := strings.TrimSuffix(origin, "/")
	for _, allowed := range s.cfg.Server.AllowedOrigins {
		if allowed != "" && strings.EqualFold(strings.TrimSuffix(allowed, "/"), trimmed) {
			return true
		}
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		return strings.EqualFold(u.Host, host)
	}
	return false
}

// hostAllowed accepts the names a loopback gateway is legitimately reached
// by: localhost and its subdomains, any loopback IP literal, and the exact
// address the operator configured. Anything else is a hostname some other
// party controls the DNS for.
func (s *Server) hostAllowed(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return false
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	listenHost := s.cfg.Server.Listen
	if h, _, err := net.SplitHostPort(listenHost); err == nil {
		listenHost = h
	}
	return listenHost != "" && strings.EqualFold(host, listenHost)
}
