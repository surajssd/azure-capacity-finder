package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"
	"github.com/surajssd/azure-capacity-finder/pkg/azure"
	"github.com/surajssd/azure-capacity-finder/pkg/capacity"
	"github.com/surajssd/azure-capacity-finder/pkg/output"
)

var (
	skuFlag           string
	subscriptionsFlag string
	regionsFlag       string
	scaleFlag         int
	parallelismFlag   int
)

var findCmd = &cobra.Command{
	Use:   "find",
	Short: "Find Azure regions with available VM capacity",
	Long:  "Search Azure regions for VM SKU availability and sufficient vCPU quota.",
	SilenceUsage: true,
	RunE:         runFind,
}

func init() {
	findCmd.Flags().StringVar(&skuFlag, "sku", "", "Comma-separated VM SKU names (required)")
	findCmd.Flags().StringVar(&subscriptionsFlag, "subscriptions", "", "Comma-separated subscription IDs")
	findCmd.Flags().StringVar(&regionsFlag, "regions", "", "Comma-separated region names")
	findCmd.Flags().IntVar(&scaleFlag, "scale", 1, "Number of VMs needed")
	findCmd.Flags().IntVar(&parallelismFlag, "parallelism", 3, "Max concurrent region checks")

	if err := findCmd.MarkFlagRequired("sku"); err != nil {
		panic(fmt.Sprintf("failed to mark flag required: %v", err))
	}

	rootCmd.AddCommand(findCmd)
}

func runFind(cmd *cobra.Command, args []string) error {
	// Set up context with signal handling for graceful cancellation.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Parse SKU names.
	skuNames := splitAndTrim(skuFlag)
	if len(skuNames) == 0 {
		return fmt.Errorf("--sku must specify at least one SKU name")
	}

	slog.Info("starting capacity search", "skus", skuNames, "scale", scaleFlag)

	// Authenticate.
	cred, err := azure.NewCredential()
	if err != nil {
		return err
	}

	// Resolve subscriptions.
	var providedSubs []string
	if subscriptionsFlag != "" {
		providedSubs = splitAndTrim(subscriptionsFlag)
	}

	subs, err := azure.ResolveSubscriptions(providedSubs)
	if err != nil {
		return err
	}

	slog.Info("using subscriptions", "count", len(subs))

	// Resolve regions.
	var regionFilter []string
	if regionsFlag != "" {
		regionFilter = splitAndTrim(regionsFlag)
	}

	regions := azure.ListRegions(regionFilter)

	slog.Info("checking regions", "count", len(regions))

	// Run capacity checks.
	input := &capacity.CheckInput{
		Subscriptions: subs,
		VMSKUs:        skuNames,
		Regions:       regions,
		Scale:         scaleFlag,
		Parallelism:   parallelismFlag,
	}

	results := capacity.Run(ctx, cred, input)

	// Print results.
	output.PrintResults(results, verbose)

	return nil
}

// splitAndTrim splits a comma-separated string and trims whitespace from each element.
func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	var result []string

	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}
