package proxy

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// Server is the HTTP/SSE reverse proxy that sits between a coding agent and a
// local CN model server. In v0.1 (m1) it is a thin reverse proxy: it forwards
// /v1/* to --upstream verbatim. The in-stream repair pipeline is wired in m2,
// where the response writer is wrapped to intercept chat-completion SSE and
// route each reassembled tool-call arguments buffer through repair.Pipeline
// before re-emitting a clean OpenAI delta stream.
//
// Keeping the type real (not a dead stub) means m2 is a diff on the handler,
// not a rewrite.
type Server struct {
	Upstream   *url.URL
	ListenAddr string
	// Pipeline is wired in m2; nil in v0.1 means "passthrough reverse proxy".
	// Typed as a *repair.Pipeline in m2 (kept untyped here so the proxy package
	// does not import repair — the m2 wiring layer owns the typed assignment).
	Pipeline any
}

// Handler returns the http.Handler for ListenAndServe.
func (s *Server) Handler() http.Handler {
	if s.Upstream == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "callpatch: upstream not configured", http.StatusBadGateway)
		})
	}
	rp := httputil.NewSingleHostReverseProxy(s.Upstream)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// m2 hook: if r.URL.Path == "/v1/chat/completions" and the upstream
		// streams SSE, wrap w in a repairing ResponseWriter that reassembles
		// tool-call deltas and runs s.Pipeline before flushing each event.
		rp.ServeHTTP(w, r)
	})
}

// ListenAndServe runs the proxy until interrupted.
func (s *Server) ListenAndServe() error {
	if s.Upstream == nil {
		return fmt.Errorf("upstream not set")
	}
	if s.ListenAddr == "" {
		s.ListenAddr = ":8080"
	}
	return http.ListenAndServe(s.ListenAddr, s.Handler())
}
