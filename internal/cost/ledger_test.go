package cost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dhivadhiva/dl_prism/internal/config"
)

func TestPrice(t *testing.T) {
	tests := []struct {
		name             string
		cost             config.Cost
		promptTokens     int
		completionTokens int
		want             float64
	}{
		{"1M input tokens at 3.0", config.Cost{InputPerMTok: 3.0}, 1_000_000, 0, 3.0},
		{"1M output tokens at 15.0", config.Cost{OutputPerMTok: 15.0}, 0, 1_000_000, 15.0},
		{"zero cost local model", config.Cost{}, 1_000_000, 1_000_000, 0},
		{"half million input at 3.0", config.Cost{InputPerMTok: 3.0}, 500_000, 0, 1.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Price(tt.cost, tt.promptTokens, tt.completionTokens)
			if got != tt.want {
				t.Errorf("Price() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOpenFreshPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "usage.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := l.MonthTotalUSD(); got != 0 {
		t.Errorf("MonthTotalUSD() = %v, want 0 for a fresh ledger", got)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Errorf("expected parent dir to be created: %v", err)
	}
}

func TestRecordThenMonthTotal(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "usage.jsonl"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := l.Record(Entry{Provider: "p", Model: "m", PromptTokens: 100, CompletionTokens: 50, CostUSD: 1.25}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := l.Record(Entry{Provider: "p", Model: "m", PromptTokens: 10, CompletionTokens: 5, CostUSD: 0.75}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if got := l.MonthTotalUSD(); got != 2.0 {
		t.Errorf("MonthTotalUSD() = %v, want 2.0", got)
	}
}

func TestEntriesFromOtherMonthsExcludedFromTotal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.jsonl")

	now := time.Now()
	otherMonth := now.AddDate(0, -2, 0) // well outside the current month

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	enc := json.NewEncoder(f)
	for _, e := range []Entry{
		{Time: now, Provider: "p", Model: "m", CostUSD: 1.0},
		{Time: otherMonth, Provider: "p", Model: "m", CostUSD: 99.0},
	} {
		if err := enc.Encode(e); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	l, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := l.MonthTotalUSD(); got != 1.0 {
		t.Errorf("MonthTotalUSD() = %v, want 1.0 (other-month entry excluded)", got)
	}
}

func TestSummaryAggregatesPerProviderModel(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "usage.jsonl"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	entries := []Entry{
		{Provider: "openai", Model: "gpt-5", PromptTokens: 100, CompletionTokens: 50, CostUSD: 1.0},
		{Provider: "openai", Model: "gpt-5", PromptTokens: 200, CompletionTokens: 100, CostUSD: 2.0},
		{Provider: "anthropic", Model: "claude-sonnet", PromptTokens: 10, CompletionTokens: 5, CostUSD: 0.5},
	}
	for _, e := range entries {
		if err := l.Record(e); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	summary, err := l.Summary()
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if len(summary) != 2 {
		t.Fatalf("len(Summary()) = %d, want 2", len(summary))
	}
	byKey := map[string]ModelUsage{}
	for _, u := range summary {
		byKey[u.Provider+"/"+u.Model] = u
	}
	gpt, ok := byKey["openai/gpt-5"]
	if !ok {
		t.Fatalf("Summary() missing openai/gpt-5 aggregate; got %+v", summary)
	}
	if gpt.Requests != 2 || gpt.PromptTokens != 300 || gpt.CompletionTokens != 150 || gpt.CostUSD != 3.0 {
		t.Errorf("gpt-5 aggregate = %+v, want requests=2 prompt=300 completion=150 cost=3.0", gpt)
	}
	claude, ok := byKey["anthropic/claude-sonnet"]
	if !ok {
		t.Fatalf("Summary() missing anthropic/claude-sonnet aggregate; got %+v", summary)
	}
	if claude.Requests != 1 || claude.CostUSD != 0.5 {
		t.Errorf("claude-sonnet aggregate = %+v, want requests=1 cost=0.5", claude)
	}
}

func TestRolloverAcrossMonthBoundary(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "usage.jsonl"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	july := time.Date(2026, time.July, 31, 23, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return july }
	l.year, l.month = july.Year(), july.Month()

	if err := l.Record(Entry{Time: july, Provider: "p", Model: "m", CostUSD: 5.0}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if got := l.MonthTotalUSD(); got != 5.0 {
		t.Fatalf("MonthTotalUSD() before rollover = %v, want 5.0", got)
	}

	august := time.Date(2026, time.August, 1, 0, 30, 0, 0, time.UTC)
	l.now = func() time.Time { return august }
	if got := l.MonthTotalUSD(); got != 0 {
		t.Errorf("MonthTotalUSD() after rollover = %v, want 0", got)
	}
	if err := l.Record(Entry{Provider: "p", Model: "m", CostUSD: 2.0}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if got := l.MonthTotalUSD(); got != 2.0 {
		t.Errorf("MonthTotalUSD() after new-month record = %v, want 2.0", got)
	}
}

func TestDailyAggregatesAcrossMultipleDays(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "usage.jsonl"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	today := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return today }

	entries := []Entry{
		{Time: today, Provider: "p", Model: "m", CostUSD: 1.0},
		{Time: today.Add(2 * time.Hour), Provider: "p", Model: "m", CostUSD: 0.5},
		{Time: today.AddDate(0, 0, -1), Provider: "p", Model: "m", CostUSD: 2.0},
	}
	for _, e := range entries {
		if err := l.Record(e); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	got, err := l.Daily(30)
	if err != nil {
		t.Fatalf("Daily: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Daily() = %+v, want 2 days", got)
	}
	if got[0].Date != "2026-08-02" || got[0].Requests != 1 || got[0].CostUSD != 2.0 {
		t.Errorf("got[0] = %+v, want {2026-08-02 2.0 1}", got[0])
	}
	if got[1].Date != "2026-08-03" || got[1].Requests != 2 || got[1].CostUSD != 1.5 {
		t.Errorf("got[1] = %+v, want {2026-08-03 1.5 2}", got[1])
	}
}

func TestDailyCutoffExcludesOldEntries(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "usage.jsonl"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	today := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return today }

	entries := []Entry{
		{Time: today, Provider: "p", Model: "m", CostUSD: 1.0},                      // day 0, in
		{Time: today.AddDate(0, 0, -2), Provider: "p", Model: "m", CostUSD: 5.0},    // day -2, in for days=3
		{Time: today.AddDate(0, 0, -3), Provider: "p", Model: "m", CostUSD: 99.0},   // day -3, out for days=3
		{Time: today.AddDate(0, 0, -30), Provider: "p", Model: "m", CostUSD: 999.0}, // well outside, out
	}
	for _, e := range entries {
		if err := l.Record(e); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	got, err := l.Daily(3)
	if err != nil {
		t.Fatalf("Daily: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Daily(3) = %+v, want 2 days (today and 2 days ago only)", got)
	}
	for _, d := range got {
		if d.CostUSD == 99.0 || d.CostUSD == 999.0 {
			t.Errorf("Daily(3) included an entry that should have been cut off: %+v", d)
		}
	}
}

func TestDailyAscendingOrder(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "usage.jsonl"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	today := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return today }

	// Record out of chronological order to prove Daily() sorts, not just echoes.
	entries := []Entry{
		{Time: today, Provider: "p", Model: "m", CostUSD: 3.0},
		{Time: today.AddDate(0, 0, -4), Provider: "p", Model: "m", CostUSD: 1.0},
		{Time: today.AddDate(0, 0, -2), Provider: "p", Model: "m", CostUSD: 2.0},
	}
	for _, e := range entries {
		if err := l.Record(e); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	got, err := l.Daily(30)
	if err != nil {
		t.Fatalf("Daily: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Daily() = %+v, want 3 days", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Date >= got[i].Date {
			t.Errorf("Daily() not ascending: %s before %s", got[i-1].Date, got[i].Date)
		}
	}
	if got[0].Date != "2026-07-30" || got[len(got)-1].Date != "2026-08-03" {
		t.Errorf("Daily() = %+v, want first=2026-07-30 last=2026-08-03", got)
	}
}

// Days with no entries are omitted, never zero-filled.
func TestDailyOmitsDaysWithoutEntries(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "usage.jsonl"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	today := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return today }

	if err := l.Record(Entry{Time: today, Provider: "p", Model: "m", CostUSD: 1.0}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got, err := l.Daily(30)
	if err != nil {
		t.Fatalf("Daily: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Daily() = %+v, want exactly 1 day (the one with an entry)", got)
	}
}
