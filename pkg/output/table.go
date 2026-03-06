package output

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/surajssd/azure-capacity-finder/pkg/capacity"
)

// PrintResults prints the capacity check results as a formatted table.
// Only regions with available capacity are shown in the table.
func PrintResults(results []*capacity.RegionResult, verbose bool) {
	var (
		available   []*capacity.RegionResult
		unavailable int
		errored     int
	)

	for _, r := range results {
		if r.Error != nil {
			errored++
			continue
		}

		if r.HasCapacity() {
			available = append(available, r)
		} else {
			unavailable++
		}
	}

	// Sort available results by region name for consistent output.
	sort.Slice(available, func(i, j int) bool {
		if available[i].Region == available[j].Region {
			return available[i].Subscription < available[j].Subscription
		}

		return available[i].Region < available[j].Region
	})

	if len(available) == 0 {
		fmt.Println("\n❌ No regions found with available capacity.")
	} else {
		fmt.Printf("\n✅ Found %d region(s) with available capacity:\n\n", len(available))

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "REGION\tSUBSCRIPTION\tSKU\tFAMILY\tvCPUs\tQUOTA FREE\tQUOTA LIMIT\tSTATUS")
		fmt.Fprintln(w, "------\t------------\t---\t------\t-----\t----------\t-----------\t------")

		for _, r := range available {
			subDisplay := truncateSubscription(r.Subscription)
			for _, sku := range r.SKUs {
				if !sku.Available {
					continue
				}

				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%d\t%d\t✅\n",
					r.Region,
					subDisplay,
					sku.SKUName,
					sku.Family,
					sku.VCPUs,
					sku.QuotaFree,
					sku.QuotaLimit,
				)
			}
		}

		w.Flush()
	}

	// Print summary of unavailable/errored regions.
	summaryParts := []string{}

	if unavailable > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d had no capacity", unavailable))
	}

	if errored > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d had errors", errored))
	}

	if len(summaryParts) > 0 {
		summary := strings.Join(summaryParts, ", ")

		fmt.Printf("\n%d other region(s): %s.", unavailable+errored, summary)

		if !verbose {
			fmt.Print(" Use --verbose for details.")
		}

		fmt.Println()
	}

	// If verbose, print details about unavailable and errored regions.
	if verbose && (unavailable > 0 || errored > 0) {
		fmt.Println("\n--- Detailed unavailable/error results ---")

		for _, r := range results {
			if r.HasCapacity() {
				continue
			}

			if r.Error != nil {
				fmt.Printf("  ❌ %s (%s): error: %v\n", r.Region, truncateSubscription(r.Subscription), r.Error)
				continue
			}

			for _, sku := range r.SKUs {
				if sku.Available {
					continue
				}

				fmt.Printf("  ⚠️  %s (%s): %s — %s\n",
					r.Region,
					truncateSubscription(r.Subscription),
					sku.SKUName,
					sku.Reason,
				)
			}
		}
	}
}

// truncateSubscription shortens a subscription ID for display.
func truncateSubscription(sub string) string {
	if len(sub) > 12 {
		return sub[:8] + "..."
	}

	return sub
}
