// Package server exposes the OpenAI-compatible HTTP surface and runs the
// failover loop around the routing engine's decisions.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/dhivadhiva/dl_prism/internal/config"
	"github.com/dhivadhiva/dl_prism/internal/cost"
	"github.com/dhivadhiva/dl_prism/internal/provider"
	"github.com/dhivadhiva/dl_prism/internal/registry"
	"github.com/dhivadhiva/dl_prism/internal/router"
	"github.com/dhivadhiva/dl_prism/ui"
)

const maxBodyBytes = 256 << 20 // generous: vision requests embed base64 images

type Server struct {
	cfg    *config.Config
	reg    *registry.Registry
	engine *router.Engine
	ledger *cost.Ledger

	// Version is optionally set by cmd/prism after New; it is surfaced by
	// GET /admin/status. Left blank it simply reports as "".
	Version string
	// start records when this Server was constructed, for uptime reporting.
	start time.Time
}

func New(cfg *config.Config, reg *registry.Registry, engine *router.Engine, ledger *cost.Ledger) *Server {
	return &Server{cfg: cfg, reg: reg, engine: engine, ledger: ledger, start: time.Now()}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok\n")
	})
	sub, _ := fs.Sub(ui.Dist, "dist")
	spa := http.FileServerFS(sub)
	mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Assets carry content hashes and may cache freely, but the document
		// must always revalidate — a cached index.html would pin an old
		// bundle against a newer gateway binary.
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		spa.ServeHTTP(w, r)
	}))
	mux.HandleFunc("POST /v1/chat/completions", s.auth(s.handleChat))
	mux.HandleFunc("POST /v1/messages", s.auth(s.handleMessages))
	mux.HandleFunc("POST /v1/messages/count_tokens", s.auth(s.handleCountTokens))
	mux.HandleFunc("GET /v1/models", s.auth(s.handleModels))
	mux.HandleFunc("POST /v1/route/explain", s.auth(s.handleExplain))
	mux.HandleFunc("GET /admin/usage", s.auth(s.handleUsage))
	mux.HandleFunc("GET /admin/status", s.auth(s.handleStatus))
	mux.HandleFunc("GET /admin/routing", s.auth(s.handleRouting))
	s.routeWorkbench(mux)
	return mux
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	if len(s.cfg.Server.APIKeys) == 0 {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			token = r.Header.Get("x-api-key")
		}
		for _, k := range s.cfg.Server.APIKeys {
			// ConstantTimeCompare reports a match for two empty strings, so a
			// blank entry in api_keys (an empty YAML list item, an unset env
			// var) would authenticate a client that presented nothing at all.
			if token == "" || k == "" {
				continue
			}
			if subtle.ConstantTimeCompare([]byte(token), []byte(k)) == 1 {
				next(w, r)
				return
			}
		}
		writeErr(w, http.StatusUnauthorized, "invalid or missing API key", "auth_error")
	}
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	req, err := provider.ParseChatRequest(body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	dec, err := s.engine.Route(req)
	if err != nil {
		writeErr(w, routingErrorStatus(err), err.Error(), "prism_routing_error")
		return
	}
	s.executeChain(w, r, req, dec, writeErr)
}

// routingErrorStatus maps a routing failure onto its HTTP status: an
// unresolvable model is a 404 listing the alternatives, anything else is a
// client error.
func routingErrorStatus(err error) int {
	var unknown *router.UnknownModelError
	if errors.As(err, &unknown) {
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}

// errorWriter writes an error envelope in the inbound protocol's shape: the
// OpenAI endpoints use writeErr, /v1/messages uses the Anthropic shape.
type errorWriter func(w http.ResponseWriter, status int, msg, kind string)

// executeChain runs a routing decision's candidate chain against w, failing
// over on retryable errors until one candidate succeeds or the chain is
// exhausted — but never once bytes have reached the client. It owns the
// X-Prism-* transparency headers and the usage ledger entry, and reports
// whether a candidate answered successfully.
func (s *Server) executeChain(w http.ResponseWriter, r *http.Request, req *provider.ChatRequest, dec *router.Decision, fail errorWriter) bool {
	var attempts []string
	for _, entry := range dec.Chain {
		adapter, err := provider.ForType(entry.Provider.Type)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error(), "prism_internal_error")
			return false
		}
		h := w.Header()
		h.Set("X-Prism-Mode", dec.Mode)
		h.Set("X-Prism-Provider", entry.Provider.Name)
		h.Set("X-Prism-Model", entry.Model.ID)
		reason := dec.Reason
		if len(attempts) > 0 {
			reason += fmt.Sprintf("; failed over after: %s", strings.Join(attempts, ", "))
		}
		h.Set("X-Prism-Reason", reason)

		rec := &recorder{ResponseWriter: w}
		res, err := adapter.ChatCompletion(entry.Provider, entry.Model.ID, req, rec, r)
		if err == nil {
			s.engine.Breaker.Success(entry.Provider.Name)
			s.record(entry, dec, req, res)
			return true
		}

		retryable := provider.Retryable(err)
		if retryable {
			s.engine.Breaker.Failure(entry.Provider.Name)
		}
		log.Printf("upstream %s/%s failed: %v", entry.Provider.Name, entry.Model.ID, err)
		if rec.wrote || !retryable {
			if !rec.wrote {
				status := http.StatusBadGateway
				var ue *provider.UpstreamError
				if !errors.As(err, &ue) {
					status = http.StatusBadRequest // adapter rejected the request pre-flight
				} else if ue.Status >= 400 && ue.Status < 500 {
					// Client-caused upstream rejections pass through; a 502
					// here would make OpenAI SDKs retry a hopeless request.
					status = ue.Status
				}
				fail(w, status, err.Error(), "upstream_error")
			}
			return false
		}
		attempts = append(attempts, fmt.Sprintf("%s/%s (%v)", entry.Provider.Name, entry.Model.ID, err))
	}
	fail(w, http.StatusBadGateway,
		fmt.Sprintf("all candidates failed: %s", strings.Join(attempts, ", ")), "upstream_error")
	return false
}

// record writes the usage ledger entry; estimates when the upstream sent no
// usage (e.g. streams without stream_options.include_usage).
func (s *Server) record(entry registry.Entry, dec *router.Decision, req *provider.ChatRequest, res *provider.Result) {
	if s.ledger == nil {
		return
	}
	e := cost.Entry{
		Provider:         entry.Provider.Name,
		Model:            entry.Model.ID,
		PromptTokens:     res.PromptTokens,
		CompletionTokens: res.CompletionTokens,
	}
	if !res.UsageKnown {
		if dec.Profile != nil {
			e.PromptTokens = dec.Profile.EstTokens
		} else {
			e.PromptTokens = router.Classify(req).EstTokens
		}
		e.Estimated = true
	}
	e.CostUSD = cost.Price(entry.Model.Cost, e.PromptTokens, e.CompletionTokens)
	if err := s.ledger.Record(e); err != nil {
		log.Printf("ledger: %v", err)
	}
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	type modelInfo struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
		Prism   any    `json:"prism,omitempty"`
	}
	data := []modelInfo{{ID: "auto", Object: "model", OwnedBy: "prism",
		Prism: map[string]any{"description": "automatic routing via policy rules"}}}
	for _, e := range s.reg.Entries() {
		data = append(data, modelInfo{
			ID: e.Model.ID, Object: "model", OwnedBy: e.Provider.Name,
			Prism: map[string]any{
				"aliases":        e.Model.Aliases,
				"local":          e.Provider.Local,
				"context_window": e.Model.ContextWindow,
				"capabilities":   e.Model.Capabilities,
				"speed":          e.Model.Speed,
				"cost_per_mtok":  map[string]float64{"input": e.Model.Cost.InputPerMTok, "output": e.Model.Cost.OutputPerMTok},
			},
		})
	}
	writeJSON(w, map[string]any{"object": "list", "data": data})
}

func (s *Server) handleExplain(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	req, err := provider.ParseChatRequest(body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	dec, err := s.engine.Route(req)
	if err != nil {
		writeErr(w, routingErrorStatus(err), err.Error(), "prism_routing_error")
		return
	}
	writeJSON(w, ExplainResponse(dec))
}

// ExplainResponse shapes a decision for JSON output (also used by the CLI).
func ExplainResponse(dec *router.Decision) map[string]any {
	chain := make([]map[string]any, 0, len(dec.Chain))
	for _, e := range dec.Chain {
		chain = append(chain, map[string]any{
			"provider": e.Provider.Name,
			"model":    e.Model.ID,
			"local":    e.Provider.Local,
		})
	}
	out := map[string]any{
		"mode":   dec.Mode,
		"reason": dec.Reason,
		"chain":  chain,
	}
	if dec.Profile != nil {
		out["profile"] = dec.Profile
		if len(dec.Chain) > 0 {
			m := dec.Chain[0].Model
			out["est_input_cost_usd"] = cost.Price(m.Cost, dec.Profile.EstTokens, 0)
		}
	}
	return out
}

const dailyUsageWindow = 30 // days shown in /admin/usage's "daily" series

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	if s.ledger == nil {
		writeJSON(w, map[string]any{
			"month_total_usd": 0.0,
			"budget_usd":      s.cfg.Budget.MonthlyUSD,
			"budget_exceeded": false,
			"models":          []cost.ModelUsage{},
			"daily":           []cost.DayUsage{},
		})
		return
	}
	models, err := s.ledger.Summary()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error(), "prism_internal_error")
		return
	}
	daily, err := s.ledger.Daily(dailyUsageWindow)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error(), "prism_internal_error")
		return
	}
	total := s.ledger.MonthTotalUSD()
	writeJSON(w, map[string]any{
		"month_total_usd": total,
		"budget_usd":      s.cfg.Budget.MonthlyUSD,
		"budget_exceeded": s.cfg.Budget.MonthlyUSD > 0 && total >= s.cfg.Budget.MonthlyUSD,
		"models":          models,
		"daily":           daily,
	})
}

// providerStatus is one entry in /admin/status's "providers" list.
type providerStatus struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Local   bool   `json:"local"`
	Healthy bool   `json:"healthy"`
	Models  int    `json:"models"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	providers := make([]providerStatus, 0, len(s.cfg.Providers))
	for i := range s.cfg.Providers {
		p := &s.cfg.Providers[i]
		healthy := true
		if s.engine != nil && s.engine.Breaker != nil {
			healthy = s.engine.Breaker.Healthy(p.Name)
		}
		providers = append(providers, providerStatus{
			Name:    p.Name,
			Type:    p.Type,
			Local:   p.Local,
			Healthy: healthy,
			Models:  len(p.Models),
		})
	}
	writeJSON(w, map[string]any{
		"version":        s.Version,
		"uptime_seconds": int(time.Since(s.start).Seconds()),
		"offline":        s.cfg.Offline,
		"listen":         s.cfg.Server.Listen,
		"providers":      providers,
	})
}

// routingRule is one entry in /admin/routing's "rules" list.
type routingRule struct {
	If   string `json:"if"`
	Then string `json:"then"`
}

func (s *Server) handleRouting(w http.ResponseWriter, r *http.Request) {
	rules := make([]routingRule, 0, len(s.cfg.Routing.Rules))
	for _, rule := range s.cfg.Routing.Rules {
		rules = append(rules, routingRule{If: rule.If.Describe(), Then: rule.Then})
	}
	fallbacks := s.cfg.Routing.Fallbacks
	if fallbacks == nil {
		fallbacks = map[string][]string{}
	}
	writeJSON(w, map[string]any{
		"default":   s.cfg.Routing.Default,
		"rules":     rules,
		"fallbacks": fallbacks,
	})
}

// recorder tracks whether anything reached the client, which is the line
// past which failover is impossible.
type recorder struct {
	http.ResponseWriter
	wrote bool
}

func (r *recorder) WriteHeader(code int) {
	r.wrote = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	r.wrote = true
	return r.ResponseWriter.Write(b)
}

func (r *recorder) Flush() {
	// Flushing commits the response (status line and headers go out even if
	// no body byte was written yet), so it counts as reaching the client.
	r.wrote = true
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg, kind string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"message": msg, "type": kind},
	})
}
