package capacity

import (
	"context"
	"log/slog"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

// Run executes capacity checks across all subscriptions and regions in parallel.
// It uses a semaphore to limit concurrency to input.Parallelism.
func Run(ctx context.Context, cred azcore.TokenCredential, input *CheckInput) []*RegionResult {
	// Create one Checker per subscription (SDK clients are goroutine-safe).
	checkers := make(map[string]*Checker)
	for _, sub := range input.Subscriptions {
		checker, err := NewChecker(sub, cred)
		if err != nil {
			slog.Error("failed to create checker", "subscription", sub, "error", err)
			continue
		}

		checkers[sub] = checker
	}

	var (
		mu      sync.Mutex
		results []*RegionResult
		wg      sync.WaitGroup
		sem     = make(chan struct{}, input.Parallelism)
	)

	for _, sub := range input.Subscriptions {
		checker, ok := checkers[sub]
		if !ok {
			continue
		}

		for _, region := range input.Regions {
			wg.Add(1)
			go func(sub, region string, checker *Checker) {
				defer wg.Done()

				// Acquire semaphore.
				sem <- struct{}{}
				defer func() { <-sem }()

				// Check for cancellation.
				if ctx.Err() != nil {
					return
				}

				slog.Debug("checking region", "subscription", sub, "region", region)
				result := checker.CheckRegion(ctx, region, input.VMSKUs, input.Scale)

				mu.Lock()
				results = append(results, result)
				mu.Unlock()

				if result.Error != nil {
					slog.Debug("region check failed",
						"subscription", sub,
						"region", region,
						"error", result.Error,
					)
				} else if result.HasCapacity() {
					slog.Info("found capacity",
						"subscription", sub,
						"region", region,
					)
				} else {
					slog.Debug("no capacity",
						"subscription", sub,
						"region", region,
					)
				}
			}(sub, region, checker)
		}
	}

	wg.Wait()
	return results
}
