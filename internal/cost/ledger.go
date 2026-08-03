// Package cost prices requests and keeps the monthly usage ledger that
// backs budget-aware routing. The ledger stores token counts and costs
// only — never prompt content.
package cost

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/dhivadhiva/dl_prism/internal/config"
)

type Entry struct {
	Time             time.Time `json:"time"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	CostUSD          float64   `json:"cost_usd"`
	// Estimated marks entries where the upstream reported no usage and
	// tokens were approximated from request size.
	Estimated bool `json:"estimated,omitempty"`
}

// Price returns USD for the given token counts at a model's configured rates.
func Price(c config.Cost, promptTokens, completionTokens int) float64 {
	return float64(promptTokens)/1e6*c.InputPerMTok + float64(completionTokens)/1e6*c.OutputPerMTok
}

// Ledger appends usage entries to a JSONL file and tracks current-month spend.
type Ledger struct {
	mu    sync.Mutex
	path  string
	year  int
	month time.Month
	total float64
	now   func() time.Time
}

// DefaultPath honours XDG_DATA_HOME, falling back to ~/.local/share.
func DefaultPath() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "prism", "usage.jsonl"), nil
}

func Open(path string) (*Ledger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	l := &Ledger{path: path, now: time.Now}
	l.year, l.month = l.now().Year(), l.now().Month()
	entries, err := l.read()
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Time.Year() == l.year && e.Time.Month() == l.month {
			l.total += e.CostUSD
		}
	}
	return l, nil
}

func (l *Ledger) read() ([]Entry, error) {
	f, err := os.Open(l.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var entries []Entry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			entries = append(entries, e)
		}
	}
	return entries, sc.Err()
}

func (l *Ledger) Record(e Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e.Time.IsZero() {
		e.Time = l.now()
	}
	l.rollover(e.Time)
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	if e.Time.Year() == l.year && e.Time.Month() == l.month {
		l.total += e.CostUSD
	}
	return nil
}

func (l *Ledger) rollover(now time.Time) {
	if now.Year() != l.year || now.Month() != l.month {
		l.year, l.month, l.total = now.Year(), now.Month(), 0
	}
}

// MonthTotalUSD is the spend recorded for the current calendar month.
func (l *Ledger) MonthTotalUSD() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rollover(l.now())
	return l.total
}

// ModelUsage is a per-model aggregate for the current month.
type ModelUsage struct {
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	Requests         int     `json:"requests"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CostUSD          float64 `json:"cost_usd"`
}

func (l *Ledger) Summary() ([]ModelUsage, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries, err := l.read()
	if err != nil {
		return nil, err
	}
	now := l.now()
	agg := map[string]*ModelUsage{}
	var order []string
	for _, e := range entries {
		if e.Time.Year() != now.Year() || e.Time.Month() != now.Month() {
			continue
		}
		key := e.Provider + "/" + e.Model
		u, ok := agg[key]
		if !ok {
			u = &ModelUsage{Provider: e.Provider, Model: e.Model}
			agg[key] = u
			order = append(order, key)
		}
		u.Requests++
		u.PromptTokens += e.PromptTokens
		u.CompletionTokens += e.CompletionTokens
		u.CostUSD += e.CostUSD
	}
	out := make([]ModelUsage, 0, len(order))
	for _, key := range order {
		out = append(out, *agg[key])
	}
	return out, nil
}

// DayUsage is a per-day aggregate used for usage charts.
type DayUsage struct {
	Date     string  `json:"date"`
	CostUSD  float64 `json:"cost_usd"`
	Requests int     `json:"requests"`
}

// Daily aggregates ledger entries by local calendar date over the last days
// days, inclusive of today, ascending by date. Days with no entries are
// omitted rather than zero-filled.
func (l *Ledger) Daily(days int) ([]DayUsage, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries, err := l.read()
	if err != nil {
		return nil, err
	}
	now := l.now()
	cutoff := now.AddDate(0, 0, -days+1)
	cutoff = time.Date(cutoff.Year(), cutoff.Month(), cutoff.Day(), 0, 0, 0, 0, cutoff.Location())

	agg := map[string]*DayUsage{}
	for _, e := range entries {
		t := e.Time.In(cutoff.Location())
		if t.Before(cutoff) {
			continue
		}
		key := t.Format("2006-01-02")
		u, ok := agg[key]
		if !ok {
			u = &DayUsage{Date: key}
			agg[key] = u
		}
		u.CostUSD += e.CostUSD
		u.Requests++
	}

	dates := make([]string, 0, len(agg))
	for d := range agg {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	out := make([]DayUsage, 0, len(dates))
	for _, d := range dates {
		out = append(out, *agg[d])
	}
	return out, nil
}

func (l *Ledger) String() string {
	return fmt.Sprintf("ledger at %s", l.path)
}
