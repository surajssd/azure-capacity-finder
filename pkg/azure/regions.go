package azure

import "log/slog"

// allRegions is a hardcoded list of all Azure physical regions.
// This avoids an API call and the subscription-level permission it requires.
// Last updated: 2026-03-06.
var allRegions = []string{
	"australiacentral",
	"australiacentral2",
	"australiaeast",
	"australiasoutheast",
	"austriaeast",
	"belgiumcentral",
	"brazilsouth",
	"brazilsoutheast",
	"canadacentral",
	"canadaeast",
	"centralindia",
	"centralus",
	"centraluseuap",
	"chilecentral",
	"denmarkeast",
	"eastasia",
	"eastus",
	"eastus2",
	"eastus2euap",
	"eastusstg",
	"francecentral",
	"francesouth",
	"germanynorth",
	"germanywestcentral",
	"indonesiacentral",
	"israelcentral",
	"italynorth",
	"japaneast",
	"japanwest",
	"jioindiacentral",
	"jioindiawest",
	"koreacentral",
	"koreasouth",
	"malaysiawest",
	"mexicocentral",
	"newzealandnorth",
	"northcentralus",
	"northeurope",
	"norwayeast",
	"norwaywest",
	"polandcentral",
	"qatarcentral",
	"southafricanorth",
	"southafricawest",
	"southcentralus",
	"southcentralusstg",
	"southeastasia",
	"southindia",
	"spaincentral",
	"swedencentral",
	"switzerlandnorth",
	"switzerlandwest",
	"uaecentral",
	"uaenorth",
	"uksouth",
	"ukwest",
	"westcentralus",
	"westeurope",
	"westindia",
	"westus",
	"westus2",
	"westus3",
}

// ListRegions returns the list of Azure regions to check.
// If a filter is provided, those regions are returned directly.
// Otherwise, the hardcoded list of all Azure physical regions is returned.
func ListRegions(filter []string) []string {
	if len(filter) > 0 {
		slog.Debug("using provided region filter", "regions", filter)
		return filter
	}

	slog.Debug("using all Azure regions", "count", len(allRegions))
	return allRegions
}
