// Command prism is DL Prism: a local-first AI gateway that routes
// OpenAI-compatible requests across cloud and local model providers.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dhivadhiva/dl_prism/internal/config"
	"github.com/dhivadhiva/dl_prism/internal/cost"
	"github.com/dhivadhiva/dl_prism/internal/mcp"
	"github.com/dhivadhiva/dl_prism/internal/provider"
	"github.com/dhivadhiva/dl_prism/internal/registry"
	"github.com/dhivadhiva/dl_prism/internal/router"
	"github.com/dhivadhiva/dl_prism/internal/server"
)

// version is overridable at build time:
//
//	go build -ldflags "-X main.version=x.y.z" ./cmd/prism
var version = "0.2.0"

const usageText = `DL Prism — local-first AI gateway

Usage:
  prism serve   [-c config] [--listen addr] [--offline]   start the gateway
  prism models  [-c config]                               list the unified model registry
  prism explain [-c config] [-m model] <prompt>           dry-run a routing decision
  prism usage   [-c config]                               show this month's spend
  prism mcp     [--gateway url] [--api-key key]           MCP server over stdio (Claude Code, Codex)
  prism version

Config search order: -c flag, ./prism.yaml, ~/.config/prism/prism.yaml
`

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		fmt.Print(usageText)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "serve":
		err = cmdServe(args)
	case "models":
		err = cmdModels(args)
	case "explain":
		err = cmdExplain(args)
	case "usage":
		err = cmdUsage(args)
	case "mcp":
		err = cmdMCP(args)
	case "version":
		fmt.Println("prism", version)
	case "help", "-h", "--help":
		fmt.Print(usageText)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usageText)
		os.Exit(2)
	}
	if err != nil {
		log.Fatalf("prism %s: %v", cmd, err)
	}
}

func findConfig(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if _, err := os.Stat("prism.yaml"); err == nil {
		return "prism.yaml", nil
	}
	home, err := os.UserHomeDir()
	if err == nil {
		p := filepath.Join(home, ".config", "prism", "prism.yaml")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no config found: pass -c, or create ./prism.yaml or ~/.config/prism/prism.yaml (see prism.example.yaml)")
}

// load builds the full stack shared by every subcommand.
func load(configPath string, offline bool) (*config.Config, *registry.Registry, *router.Engine, *cost.Ledger, error) {
	path, err := findConfig(configPath)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if offline {
		cfg.Offline = true
	}
	reg, err := registry.New(cfg)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	ledgerPath, err := cost.DefaultPath()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	ledger, err := cost.Open(ledgerPath)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	engine := &router.Engine{
		Cfg:     cfg,
		Reg:     reg,
		Breaker: router.NewBreaker(),
		BudgetExceeded: func() bool {
			return cfg.Budget.MonthlyUSD > 0 && ledger.MonthTotalUSD() >= cfg.Budget.MonthlyUSD
		},
	}
	if err := engine.Validate(); err != nil {
		return nil, nil, nil, nil, err
	}
	return cfg, reg, engine, ledger, nil
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("c", "", "config file")
	listen := fs.String("listen", "", "override server.listen")
	offline := fs.Bool("offline", false, "route to local providers only")
	fs.Parse(args)

	cfg, reg, engine, ledger, err := load(*configPath, *offline)
	if err != nil {
		return err
	}
	if *listen != "" {
		cfg.Server.Listen = *listen
	}
	srv := server.New(cfg, reg, engine, ledger)
	srv.Version = version
	mode := "online"
	if cfg.Offline {
		mode = "offline (local providers only)"
	}
	log.Printf("prism %s listening on http://%s (%d models, %s)", version, cfg.Server.Listen, len(reg.Entries()), mode)
	log.Printf("point any OpenAI-compatible client at base_url http://%s/v1", cfg.Server.Listen)
	return http.ListenAndServe(cfg.Server.Listen, srv.Handler())
}

func cmdModels(args []string) error {
	fs := flag.NewFlagSet("models", flag.ExitOnError)
	configPath := fs.String("c", "", "config file")
	fs.Parse(args)

	_, reg, _, _, err := load(*configPath, false)
	if err != nil {
		return err
	}
	fmt.Printf("%-28s %-12s %-6s %-9s %-22s %s\n", "MODEL", "PROVIDER", "LOCAL", "CONTEXT", "COST $/MTOK (in/out)", "ALIASES")
	for _, e := range reg.Entries() {
		local := ""
		if e.Provider.Local {
			local = "yes"
		}
		fmt.Printf("%-28s %-12s %-6s %-9d %-22s %s\n",
			e.Model.ID, e.Provider.Name, local, e.Model.ContextWindow,
			fmt.Sprintf("%.2f / %.2f", e.Model.Cost.InputPerMTok, e.Model.Cost.OutputPerMTok),
			strings.Join(e.Model.Aliases, ", "))
	}
	return nil
}

func cmdExplain(args []string) error {
	fs := flag.NewFlagSet("explain", flag.ExitOnError)
	configPath := fs.String("c", "", "config file")
	model := fs.String("m", "auto", "model to resolve (default: automatic routing)")
	fs.Parse(args)
	prompt := strings.Join(fs.Args(), " ")
	if prompt == "" {
		return fmt.Errorf("usage: prism explain [-m model] <prompt>")
	}

	_, _, engine, _, err := load(*configPath, false)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{
		"model":    *model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	req, err := provider.ParseChatRequest(body)
	if err != nil {
		return err
	}
	dec, err := engine.Route(req)
	if err != nil {
		return err
	}
	out, _ := json.MarshalIndent(server.ExplainResponse(dec), "", "  ")
	fmt.Println(string(out))
	return nil
}

// defaultGateway is where `prism serve` listens out of the box.
const defaultGateway = "http://127.0.0.1:8130"

// cmdMCP speaks MCP over stdio for hosts like Claude Code. It talks to a
// *running* gateway over HTTP rather than loading the config itself: the
// gateway stays the single source of truth for routing, breaker and ledger
// state, and the subcommand works from any directory without a prism.yaml.
func cmdMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	gateway := fs.String("gateway", "", "gateway base URL (default $PRISM_GATEWAY, else "+defaultGateway+")")
	apiKey := fs.String("api-key", "", "gateway API key (default $PRISM_API_KEY)")
	fs.Parse(args)

	base := normalizeURL(firstNonEmpty(*gateway, os.Getenv("PRISM_GATEWAY"), defaultGateway))
	// stdout carries the JSON-RPC stream and nothing else.
	log.SetOutput(os.Stderr)
	log.Printf("prism mcp %s: serving MCP over stdio against %s", version, base)
	srv := &mcp.Server{
		Gateway: base,
		APIKey:  firstNonEmpty(*apiKey, os.Getenv("PRISM_API_KEY")),
		Version: version,
		Logf:    log.Printf,
	}
	return srv.Serve(os.Stdin, os.Stdout)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// normalizeURL accepts a bare host:port, which is what people type.
func normalizeURL(u string) string {
	if !strings.Contains(u, "://") {
		return "http://" + u
	}
	return u
}

func cmdUsage(args []string) error {
	fs := flag.NewFlagSet("usage", flag.ExitOnError)
	configPath := fs.String("c", "", "config file")
	fs.Parse(args)

	cfg, _, _, ledger, err := load(*configPath, false)
	if err != nil {
		return err
	}
	models, err := ledger.Summary()
	if err != nil {
		return err
	}
	fmt.Printf("This month: $%.4f", ledger.MonthTotalUSD())
	if cfg.Budget.MonthlyUSD > 0 {
		fmt.Printf(" of $%.2f budget", cfg.Budget.MonthlyUSD)
	}
	fmt.Println()
	if len(models) == 0 {
		fmt.Println("No usage recorded yet.")
		return nil
	}
	fmt.Printf("%-28s %-12s %-9s %-12s %-12s %s\n", "MODEL", "PROVIDER", "REQUESTS", "TOK IN", "TOK OUT", "COST USD")
	for _, m := range models {
		fmt.Printf("%-28s %-12s %-9d %-12d %-12d %.4f\n",
			m.Model, m.Provider, m.Requests, m.PromptTokens, m.CompletionTokens, m.CostUSD)
	}
	return nil
}
