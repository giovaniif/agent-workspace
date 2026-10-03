package domain

import "testing"

func lowClaudeQuotas(claudeLeft, codexLeft int) []Quota {
	return []Quota{
		{Harness: HarnessClaude, Window: "five_hour", LeftPercent: claudeLeft},
		{Harness: HarnessCodex, Window: "five_hour", LeftPercent: codexLeft},
	}
}

func TestFallbackOfferMapsModelAndEffortToCodex(t *testing.T) {
	cfg := FallbackConfig{Models: map[string]string{"opus": "gpt-5", "sonnet": "gpt-5-mini"}, Efforts: map[string]string{"high": "medium"}}
	got, ok := OfferFallback(lowClaudeQuotas(10, 80), cfg, StartRequest{Harness: HarnessClaude, Model: "claude-opus-4-7", Effort: "high"})
	if !ok {
		t.Fatal("no offer")
	}
	if want := (StartRequest{Harness: HarnessCodex, Model: "gpt-5", Effort: "medium"}); got.Request != want {
		t.Fatalf("request %+v, want %+v", got.Request, want)
	}
	if got.Advice.Low.LeftPercent != 10 || got.Advice.OtherShortest.LeftPercent != 80 {
		t.Fatalf("advice %+v", got.Advice)
	}
}

func TestFallbackOfferKeepsAKnownEffortAndDropsOneCodexLacks(t *testing.T) {
	for effort, want := range map[string]string{"low": "low", "medium": "medium", "high": "high", "xhigh": "high", "max": "high", "": "", "turbo": ""} {
		got, ok := OfferFallback(lowClaudeQuotas(10, 80), FallbackConfig{}, StartRequest{Harness: HarnessClaude, Effort: effort})
		if !ok || got.Request.Effort != want {
			t.Errorf("effort %q -> %q (ok=%v), want %q", effort, got.Request.Effort, ok, want)
		}
	}
}

func TestFallbackOfferLeavesAnUnmappedModelToCodexsDefault(t *testing.T) {
	got, ok := OfferFallback(lowClaudeQuotas(10, 80), FallbackConfig{Models: map[string]string{"opus": "gpt-5"}}, StartRequest{Harness: HarnessClaude, Model: "haiku"})
	if !ok || got.Request.Model != "" {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
}

func TestFallbackOfferUsesTheDefaultModelEntryForARequestWithNoModel(t *testing.T) {
	got, _ := OfferFallback(lowClaudeQuotas(10, 80), FallbackConfig{Models: map[string]string{"default": "gpt-5"}}, StartRequest{Harness: HarnessClaude})
	if got.Request.Model != "gpt-5" {
		t.Fatalf("got %+v", got.Request)
	}
}

func TestFallbackOfferPrefersTheLongestMatchingModelKey(t *testing.T) {
	cfg := FallbackConfig{Models: map[string]string{"opus": "a", "opus-4-7": "b"}}
	got, _ := OfferFallback(lowClaudeQuotas(10, 80), cfg, StartRequest{Harness: HarnessClaude, Model: "Claude-Opus-4-7"})
	if got.Request.Model != "b" {
		t.Fatalf("got %+v", got.Request)
	}
}

func TestFallbackThresholdIsConfigurable(t *testing.T) {
	quotas := lowClaudeQuotas(30, 80)
	req := StartRequest{Harness: HarnessClaude}
	if _, ok := OfferFallback(quotas, FallbackConfig{}, req); ok {
		t.Fatal("offered at 30% with the default threshold")
	}
	if _, ok := OfferFallback(quotas, FallbackConfig{Threshold: 40}, req); !ok {
		t.Fatal("not offered at 30% with a threshold of 40")
	}
	if _, ok := OfferFallback(lowClaudeQuotas(10, 80), FallbackConfig{Threshold: 10}, req); ok {
		t.Fatal("offered at exactly the threshold")
	}
}

func TestFallbackOfferOnlyForClaudeRequests(t *testing.T) {
	quotas := []Quota{
		{Harness: HarnessClaude, Window: "five_hour", LeftPercent: 80},
		{Harness: HarnessCodex, Window: "five_hour", LeftPercent: 5},
	}
	if got, ok := OfferFallback(quotas, FallbackConfig{}, StartRequest{Harness: HarnessCodex}); ok {
		t.Fatalf("offered a switch to claude: %+v", got)
	}
}

func TestFallbackOfferNeedsACodexFigureThatIsNotLowItself(t *testing.T) {
	req := StartRequest{Harness: HarnessClaude}
	if _, ok := OfferFallback(lowClaudeQuotas(10, 5), FallbackConfig{}, req); ok {
		t.Fatal("offered codex with 5% left")
	}
	if _, ok := OfferFallback(lowClaudeQuotas(10, 25), FallbackConfig{}, req); !ok {
		t.Fatal("refused codex at exactly the threshold")
	}
	if _, ok := OfferFallback([]Quota{{Harness: HarnessClaude, Window: "five_hour", LeftPercent: 10}}, FallbackConfig{}, req); ok {
		t.Fatal("offered codex with no figure")
	}
}

func TestFallbackOffersForAQueueAnswerPerRequest(t *testing.T) {
	queue := []StartRequest{
		{Harness: HarnessClaude, Model: "opus"},
		{Harness: HarnessCodex, Model: "gpt-5"},
		{Harness: HarnessClaude, Model: "sonnet"},
	}
	got := OfferFallbacks(lowClaudeQuotas(10, 80), FallbackConfig{Models: map[string]string{"opus": "gpt-5-pro"}}, queue)
	if len(got) != 2 || got[0].Request.Model != "gpt-5-pro" || got[2].Request.Model != "" {
		t.Fatalf("got %+v", got)
	}
	if _, ok := got[1]; ok {
		t.Fatal("offered a fallback for a codex request")
	}
}

func TestFallbackOfferStaysOnCodexWhenOmpHasMoreLeft(t *testing.T) {
	quotas := append(lowClaudeQuotas(10, 50), Quota{Harness: HarnessOmp, Window: "five_hour", LeftPercent: 90})
	got, ok := OfferFallback(quotas, FallbackConfig{}, StartRequest{Harness: HarnessClaude})
	if !ok || got.Request.Harness != HarnessCodex || got.Advice.Other != HarnessCodex || got.Advice.OtherShortest.LeftPercent != 50 {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
	if _, ok := OfferFallback(append(lowClaudeQuotas(10, 5), quotas[2]), FallbackConfig{}, StartRequest{Harness: HarnessClaude}); ok {
		t.Fatal("offered a low Codex because omp has room")
	}
}
