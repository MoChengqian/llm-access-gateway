package main

import "testing"

func TestParseRuleSpecSupportsExactAndGenericRules(t *testing.T) {
	exact, err := parseRuleSpec("fast-gpt4o,gpt-4o-mini,10")
	if err != nil {
		t.Fatalf("parse exact rule: %v", err)
	}
	if exact.BackendName != "fast-gpt4o" || exact.Model != "gpt-4o-mini" || exact.Priority != 10 {
		t.Fatalf("unexpected exact rule %#v", exact)
	}

	generic, err := parseRuleSpec("generic-fallback,,20")
	if err != nil {
		t.Fatalf("parse generic rule: %v", err)
	}
	if generic.BackendName != "generic-fallback" || generic.Model != "" || generic.Priority != 20 {
		t.Fatalf("unexpected generic rule %#v", generic)
	}
}

func TestParseRuleSpecRejectsBadInput(t *testing.T) {
	if _, err := parseRuleSpec("missing-parts"); err == nil {
		t.Fatal("expected invalid rule error")
	}
	if _, err := parseRuleSpec(",gpt-4o-mini,10"); err == nil {
		t.Fatal("expected missing backend error")
	}
	if _, err := parseRuleSpec("fast-gpt4o,gpt-4o-mini,abc"); err == nil {
		t.Fatal("expected bad priority error")
	}
}
