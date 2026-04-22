package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"
	"github.com/surajssd/azure-capacity-finder/pkg/azure"
	"github.com/surajssd/azure-capacity-finder/pkg/whouses"
)

var (
	whoUsesSkuFlag     string
	whoUsesSubFlag     string
	whoUsesRefreshFlag bool
)

var whoUsesCmd = &cobra.Command{
	Use:          "who-uses",
	Short:        "Find VMs and VMSSs using a specific VM SKU",
	Long:         "Search the current subscription for VMs and VMSSs that are using a specific VM SKU size. Also detects AKS clusters and provides portal links.",
	SilenceUsage: true,
	RunE:         runWhoUses,
}

func init() {
	whoUsesCmd.Flags().StringVar(&whoUsesSkuFlag, "sku", "", "VM SKU name to search for (required)")
	whoUsesCmd.Flags().StringVar(&whoUsesSubFlag, "subscription", "", "Subscription ID (defaults to current)")
	whoUsesCmd.Flags().BoolVar(&whoUsesRefreshFlag, "refresh", false, "Refresh cached VM/VMSS data")

	if err := whoUsesCmd.MarkFlagRequired("sku"); err != nil {
		panic(fmt.Sprintf("failed to mark flag required: %v", err))
	}

	rootCmd.AddCommand(whoUsesCmd)
}

func runWhoUses(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cred, err := azure.NewCredential()
	if err != nil {
		return err
	}

	// Resolve subscription.
	var providedSubs []string
	if whoUsesSubFlag != "" {
		providedSubs = []string{whoUsesSubFlag}
	}

	subs, err := azure.ResolveSubscriptions(providedSubs)
	if err != nil {
		return err
	}

	if len(subs) == 0 {
		return fmt.Errorf("no subscription found")
	}

	// Use the first subscription.
	subscriptionID := subs[0]
	fmt.Printf("⏳ Searching subscription %s for SKU %s...\n", subscriptionID, whoUsesSkuFlag)

	result, err := whouses.Run(ctx, cred, subscriptionID, whoUsesSkuFlag, whoUsesRefreshFlag)
	if err != nil {
		return err
	}

	whouses.PrintResults(result)

	return nil
}
