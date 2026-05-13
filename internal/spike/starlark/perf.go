package starlark_spike

import (
	"fmt"
	"sort"
	"time"
)

const (
	benchmarkIterations = 1000
)

// PerfResult holds the results of the performance benchmark.
type PerfResult struct {
	Iterations int
	Durations  []time.Duration
	Mean       time.Duration
	P95        time.Duration
	P99        time.Duration
	Total      time.Duration
}

// RunPerfBenchmark executes the realistic example script 1,000 times sequentially
// and records per-invocation latency. This provides order-of-magnitude confidence
// in the < 100ms p99 NFR-P4 target.
//
// Each iteration is a fresh execution: compile + exec + call + parse.
// This represents the worst-case scenario (no caching). Story 1.6 may introduce
// program caching (compile once, execute many times) which would be materially faster.
func RunPerfBenchmark() (*PerfResult, error) {
	fmt.Printf("=== PERFORMANCE BENCHMARK (%d sequential invocations) ===\n", benchmarkIterations)
	fmt.Println()
	fmt.Println("Script: RealisticExampleScript (CreateIdentity)")
	fmt.Println("Mode: full compile + execute per iteration (worst case, no caching)")
	fmt.Println()

	ctx := buildAPIErgonomicsContext()
	durations := make([]time.Duration, 0, benchmarkIterations)

	total := time.Duration(0)
	for i := 0; i < benchmarkIterations; i++ {
		start := time.Now()
		_, err := RunScript(RealisticExampleScript, ctx)
		elapsed := time.Since(start)

		if err != nil {
			return nil, fmt.Errorf("iteration %d failed: %w", i, err)
		}

		durations = append(durations, elapsed)
		total += elapsed
	}

	result := computeStats(durations, total)
	printPerfResults(result)
	return result, nil
}

func computeStats(durations []time.Duration, total time.Duration) *PerfResult {
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	n := len(sorted)
	mean := total / time.Duration(n)

	// p95: 95th percentile (index = floor(0.95 * n))
	p95Idx := int(float64(n) * 0.95)
	if p95Idx >= n {
		p95Idx = n - 1
	}
	p95 := sorted[p95Idx]

	// p99: 99th percentile
	p99Idx := int(float64(n) * 0.99)
	if p99Idx >= n {
		p99Idx = n - 1
	}
	p99 := sorted[p99Idx]

	return &PerfResult{
		Iterations: n,
		Durations:  durations,
		Mean:       mean,
		P95:        p95,
		P99:        p99,
		Total:      total,
	}
}

func printPerfResults(r *PerfResult) {
	fmt.Printf("Total time for %d iterations: %v\n", r.Iterations, r.Total)
	fmt.Printf("Mean invocation latency:      %v\n", r.Mean)
	fmt.Printf("p95 invocation latency:       %v\n", r.P95)
	fmt.Printf("p99 invocation latency:       %v\n", r.P99)
	fmt.Println()

	threshold := 100 * time.Millisecond
	if r.P99 < threshold {
		fmt.Printf("NFR-P4 threshold (< 100ms p99): WITHIN BUDGET (p99=%v)\n", r.P99)
		fmt.Println("Order-of-magnitude confidence: GO — p99 well below 100ms on dev hardware")
	} else {
		fmt.Printf("NFR-P4 threshold (< 100ms p99): OVER BUDGET (p99=%v > 100ms)\n", r.P99)
		fmt.Println("Order-of-magnitude confidence: CAUTION — see README for proposed mitigations")
	}
	fmt.Println()
}
