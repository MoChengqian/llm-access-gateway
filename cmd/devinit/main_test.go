package main

import "testing"

func TestParseFlagsDefaults(t *testing.T) {
	t.Parallel()

	limits, err := parseFlags(nil)
	if err != nil {
		t.Fatalf("parseFlags() error = %v", err)
	}

	if limits.RPMLimit != defaultRPMSeedLimit {
		t.Fatalf("RPMLimit = %d, want %d", limits.RPMLimit, defaultRPMSeedLimit)
	}
	if limits.TPMLimit != defaultTPMSeedLimit {
		t.Fatalf("TPMLimit = %d, want %d", limits.TPMLimit, defaultTPMSeedLimit)
	}
	if limits.TokenBudget != defaultTokenSeedBudget {
		t.Fatalf("TokenBudget = %d, want %d", limits.TokenBudget, defaultTokenSeedBudget)
	}
}

func TestParseFlagsOverrides(t *testing.T) {
	t.Parallel()

	limits, err := parseFlags([]string{
		"-rpm-limit=1000",
		"-tpm-limit=100000",
		"-token-budget=2000000",
	})
	if err != nil {
		t.Fatalf("parseFlags() error = %v", err)
	}

	if limits.RPMLimit != 1000 {
		t.Fatalf("RPMLimit = %d, want 1000", limits.RPMLimit)
	}
	if limits.TPMLimit != 100000 {
		t.Fatalf("TPMLimit = %d, want 100000", limits.TPMLimit)
	}
	if limits.TokenBudget != 2000000 {
		t.Fatalf("TokenBudget = %d, want 2000000", limits.TokenBudget)
	}
}

func TestParseFlagsRejectsNegativeValues(t *testing.T) {
	t.Parallel()

	testCases := [][]string{
		{"-rpm-limit=-1"},
		{"-tpm-limit=-1"},
		{"-token-budget=-1"},
	}

	for _, args := range testCases {
		args := args
		t.Run(args[0], func(t *testing.T) {
			t.Parallel()
			if _, err := parseFlags(args); err == nil {
				t.Fatalf("parseFlags(%v) error = nil, want non-nil", args)
			}
		})
	}
}
