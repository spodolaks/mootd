// Command eval runs the prompt-eval harness against a golden set
// (mootd#59).
//
// Usage:
//
//	# CI smoke test — no real LLM, just verifies the prompt
//	# construction + sanitisation layer didn't regress.
//	go run ./cmd/eval --golden=eval/golden/v1
//
//	# Verbose: dump rendered prompts + checks per tuple.
//	go run ./cmd/eval --golden=eval/golden/v1 --verbose
//
// Live LLM mode (paid API calls) is intentionally NOT implemented in
// this iteration — it requires API keys, billing oversight, and a
// way to compare against a saved baseline. Tracked separately;
// today the CI-friendly mock-mode is what we ship.
//
// Exit codes:
//
//	0 — all checks pass
//	1 — one or more checks failed (smoke regression)
//	2 — runner error (golden set unreadable, etc.)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"mootd/backend/eval"
)

func main() {
	goldenDir := flag.String("golden", "eval/golden/v1", "path to a directory containing golden-set JSON tuples")
	verbose := flag.Bool("verbose", false, "dump full prompts + per-tuple checks")
	asJSON := flag.Bool("json", false, "emit machine-readable JSON instead of human-readable summary")
	flag.Parse()

	tuples, err := eval.LoadGoldenSet(*goldenDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "eval: load golden: %v\n", err)
		os.Exit(2)
	}
	if len(tuples) == 0 {
		fmt.Fprintf(os.Stderr, "eval: no tuples found in %s\n", *goldenDir)
		os.Exit(2)
	}

	results := make([]eval.Result, 0, len(tuples))
	for _, t := range tuples {
		// Generator is intentionally nil — prompt-only smoke test.
		// Live mode (a real Generator injected here) is a follow-up.
		r := eval.Run(eval.RunOptions{Tuple: t, Verbose: *verbose})
		results = append(results, r)
	}

	if *asJSON {
		out, _ := json.MarshalIndent(results, "", "  ")
		fmt.Println(string(out))
		// Exit code still reflects pass/fail.
		if anyFailed(results) {
			os.Exit(1)
		}
		return
	}

	// Human-readable summary.
	totalChecks, failedChecks := 0, 0
	for _, r := range results {
		fmt.Printf("\n%s — %d checks\n", r.TupleID, len(r.Checks))
		for _, c := range r.Checks {
			marker := "✓"
			if !c.Pass {
				marker = "✗"
				failedChecks++
			}
			totalChecks++
			fmt.Printf("  %s  %-50s %s\n", marker, c.Name, c.Detail)
		}
		if *verbose {
			fmt.Printf("    system tokens ≈ %d / user tokens ≈ %d\n", r.SystemTokens, r.UserTokens)
		}
	}

	fmt.Printf("\nResult: %d/%d checks passed\n", totalChecks-failedChecks, totalChecks)
	if failedChecks > 0 {
		os.Exit(1)
	}
}

func anyFailed(results []eval.Result) bool {
	for _, r := range results {
		for _, c := range r.Checks {
			if !c.Pass {
				return true
			}
		}
	}
	return false
}
