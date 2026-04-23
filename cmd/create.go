package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"

	"github.com/spf13/cobra"
	"github.com/surajssd/azure-capacity-finder/pkg/azure"
	"github.com/surajssd/azure-capacity-finder/pkg/capacity"
	"github.com/surajssd/azure-capacity-finder/pkg/output"
	"github.com/surajssd/azure-capacity-finder/pkg/provisioner"
)

var (
	createSKUFlag           string
	createSubscriptionsFlag string
	createRegionsFlag       string
	createZonesFlag         string
	createScaleFlag         int
	createPrefixFlag        string
	createParallelismFlag   int
	createForceFlag         bool
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Validate capacity by creating a VMSS in available regions",
	Long: `Create a Virtual Machine Scale Set (VMSS) in regions with available quota
to validate real physical capacity. The VMSS is automatically deleted after
successful provisioning.

This command first runs the same quota checks as 'find', then sequentially
attempts to create a VMSS in each available region until one succeeds.`,
	SilenceUsage: true,
	RunE:         runCreate,
}

func init() {
	createCmd.Flags().StringVar(&createSKUFlag, "sku", "", "VM SKU name (required)")
	createCmd.Flags().StringVar(&createSubscriptionsFlag, "subscriptions", "", "Comma-separated subscription IDs")
	createCmd.Flags().StringVar(&createRegionsFlag, "regions", "", "Comma-separated region names")
	createCmd.Flags().StringVar(&createZonesFlag, "zones", "", "Comma-separated availability zones (e.g. 1,2,3)")
	createCmd.Flags().IntVar(&createScaleFlag, "scale", 1, "Number of VM instances in the VMSS")
	createCmd.Flags().StringVar(&createPrefixFlag, "prefix", "acf", "Prefix for all resource names (RG, VNet, VMSS)")
	createCmd.Flags().IntVar(&createParallelismFlag, "parallelism", 3, "Max concurrent region checks (quota phase only)")
	createCmd.Flags().BoolVar(&createForceFlag, "force", false, "Skip capacity/quota checks and attempt VMSS creation directly")

	if err := createCmd.MarkFlagRequired("sku"); err != nil {
		panic(fmt.Sprintf("failed to mark flag required: %v", err))
	}

	rootCmd.AddCommand(createCmd)
}

func runCreate(cmd *cobra.Command, args []string) error {
	// Validate flag combinations.
	if createZonesFlag != "" && createRegionsFlag == "" {
		return fmt.Errorf("--zones requires --regions to be set")
	}

	// Set up context with signal handling for graceful cancellation.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Parse SKU name (single SKU for create).
	skuName := createSKUFlag

	slog.Info("starting capacity validation", "sku", skuName, "scale", createScaleFlag)

	// Authenticate.
	cred, err := azure.NewCredential()
	if err != nil {
		return err
	}

	// Resolve subscriptions.
	var providedSubs []string
	if createSubscriptionsFlag != "" {
		providedSubs = splitAndTrim(createSubscriptionsFlag)
	}

	subs, err := azure.ResolveSubscriptions(providedSubs)
	if err != nil {
		return err
	}

	slog.Info("using subscriptions", "count", len(subs))

	// Resolve regions.
	var regionFilter []string
	if createRegionsFlag != "" {
		regionFilter = splitAndTrim(createRegionsFlag)
	}

	regions := azure.ListRegions(regionFilter)

	slog.Info("checking regions", "count", len(regions))

	// Filter to regions with capacity, sorted by region name.
	type regionCandidate struct {
		region       string
		subscription string
	}

	var candidates []regionCandidate

	if createForceFlag {
		// Skip capacity checks; treat all subscription/region combos as candidates.
		slog.Info("skipping capacity checks (--force)")
		for _, sub := range subs {
			for _, region := range regions {
				candidates = append(candidates, regionCandidate{
					region:       region,
					subscription: sub,
				})
			}
		}
	} else {
		// Run capacity checks (reuse find logic).
		input := &capacity.CheckInput{
			Subscriptions: subs,
			VMSKUs:        []string{skuName},
			Regions:       regions,
			Scale:         createScaleFlag,
			Parallelism:   createParallelismFlag,
		}

		results := capacity.Run(ctx, cred, input)

		// Display quota results (reuse find output).
		output.PrintResults(results, verbose)

		for _, r := range results {
			if r.HasCapacity() {
				candidates = append(candidates, regionCandidate{
					region:       r.Region,
					subscription: r.Subscription,
				})
			}
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].region == candidates[j].region {
			return candidates[i].subscription < candidates[j].subscription
		}
		return candidates[i].region < candidates[j].region
	})

	if len(candidates) == 0 {
		return fmt.Errorf("no regions with available capacity to attempt VMSS creation")
	}

	// Parse zones if provided.
	var zones []string
	if createZonesFlag != "" {
		zones = splitAndTrim(createZonesFlag)
	}

	// Sequential provisioning loop: try each region until one succeeds.
	for _, c := range candidates {
		p, err := provisioner.NewProvisioner(c.subscription, createPrefixFlag, cred)
		if err != nil {
			slog.Error("failed to create provisioner", "subscription", c.subscription, "error", err)
			continue
		}

		err = p.Provision(ctx, c.region, skuName, createScaleFlag, zones)
		if err == nil {
			fmt.Printf("✅ Capacity validated in %s! VMSS provisioned and cleaned up.\n", c.region)
			return nil
		}

		// Log the failure and continue to the next region.
		slog.Debug("provisioning failed", "region", c.region, "error", err)
	}

	return fmt.Errorf("VMSS creation failed in all available regions")
}
