package azure

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
)

// ListRegions returns the list of Azure regions to check.
// If a filter is provided, those regions are returned directly.
// Otherwise, all regions for the subscription are queried via the API.
func ListRegions(ctx context.Context, cred azcore.TokenCredential, subscriptionID string, filter []string) ([]string, error) {
	if len(filter) > 0 {
		slog.Debug("using provided region filter", "regions", filter)
		return filter, nil
	}

	client, err := armsubscriptions.NewClient(cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create subscriptions client: %w", err)
	}

	var regions []string
	pager := client.NewListLocationsPager(subscriptionID, nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list locations: %w", err)
		}

		for _, loc := range page.Value {
			if loc.Type == nil || *loc.Type != armsubscriptions.LocationTypeRegion {
				continue
			}

			if loc.Name == nil {
				continue
			}

			regions = append(regions, *loc.Name)
		}
	}

	slog.Debug("discovered Azure regions", "count", len(regions))
	return regions, nil
}
