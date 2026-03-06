package capacity

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v6"
)

// Checker performs capacity checks for a single subscription.
// The underlying Azure SDK clients are goroutine-safe.
type Checker struct {
	subscriptionID string
	skuClient      *armcompute.ResourceSKUsClient
	usageClient    *armcompute.UsageClient
}

// NewChecker creates a new Checker for the given subscription.
func NewChecker(subscriptionID string, cred azcore.TokenCredential) (*Checker, error) {
	skuClient, err := armcompute.NewResourceSKUsClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create ResourceSKUs client: %w", err)
	}

	usageClient, err := armcompute.NewUsageClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Usage client: %w", err)
	}

	return &Checker{
		subscriptionID: subscriptionID,
		skuClient:      skuClient,
		usageClient:    usageClient,
	}, nil
}

// skuInfo holds extracted information about a SKU from the ResourceSKU API.
type skuInfo struct {
	family     string
	vcpus      int
	restricted bool
	reason     string
}

// CheckRegion checks SKU availability and quota for the given region.
func (c *Checker) CheckRegion(ctx context.Context, region string, skuNames []string, scale int) *RegionResult {
	result := &RegionResult{
		Subscription: c.subscriptionID,
		Region:       region,
	}

	// Step 1: Query ResourceSKU API for this region.
	skuInfoMap, err := c.fetchSKUInfo(ctx, region, skuNames)
	if err != nil {
		result.Error = fmt.Errorf("failed to fetch SKU info: %w", err)
		return result
	}

	// Step 2: Query Usage API for quota data.
	quotaMap, err := c.fetchQuota(ctx, region)
	if err != nil {
		result.Error = fmt.Errorf("failed to fetch quota: %w", err)
		return result
	}

	// Step 3: Build results for each requested SKU.
	for _, skuName := range skuNames {
		skuResult := SKUResult{
			SKUName: skuName,
		}

		info, found := skuInfoMap[strings.ToLower(skuName)]
		if !found {
			skuResult.Available = false
			skuResult.Reason = "SKU not found in region"
			result.SKUs = append(result.SKUs, skuResult)
			continue
		}

		if info.restricted {
			skuResult.Available = false
			skuResult.Reason = info.reason
			skuResult.Family = info.family
			skuResult.VCPUs = info.vcpus
			result.SKUs = append(result.SKUs, skuResult)
			continue
		}

		skuResult.Family = info.family
		skuResult.VCPUs = info.vcpus

		// Look up quota for this family.
		quota, hasQuota := quotaMap[strings.ToLower(info.family)]
		if !hasQuota {
			skuResult.Available = false
			skuResult.Reason = "quota family not found"
			result.SKUs = append(result.SKUs, skuResult)
			continue
		}

		skuResult.QuotaLimit = quota.limit
		skuResult.QuotaUsed = quota.used
		skuResult.QuotaFree = quota.limit - quota.used
		skuResult.QuotaNeeded = int64(scale) * int64(info.vcpus)

		if skuResult.QuotaFree >= skuResult.QuotaNeeded {
			skuResult.Available = true
			skuResult.Reason = "available"
		} else {
			skuResult.Available = false
			skuResult.Reason = fmt.Sprintf("insufficient quota (free: %d, needed: %d)", skuResult.QuotaFree, skuResult.QuotaNeeded)
		}

		result.SKUs = append(result.SKUs, skuResult)
	}

	return result
}

// fetchSKUInfo queries the ResourceSKU API and returns information about the requested SKUs.
func (c *Checker) fetchSKUInfo(ctx context.Context, region string, skuNames []string) (map[string]*skuInfo, error) {
	// Build a set of requested SKU names (lowercase for case-insensitive matching).
	requested := make(map[string]bool, len(skuNames))
	for _, name := range skuNames {
		requested[strings.ToLower(name)] = true
	}

	result := make(map[string]*skuInfo)
	filter := fmt.Sprintf("location eq '%s'", region)

	pager := c.skuClient.NewListPager(&armcompute.ResourceSKUsClientListOptions{
		Filter: &filter,
	})

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list resource SKUs: %w", err)
		}

		for _, sku := range page.Value {
			if sku.ResourceType == nil || *sku.ResourceType != "virtualMachines" {
				continue
			}

			if sku.Name == nil {
				continue
			}

			skuNameLower := strings.ToLower(*sku.Name)
			if !requested[skuNameLower] {
				continue
			}

			info := &skuInfo{}

			// Extract family.
			if sku.Family != nil {
				info.family = *sku.Family
			}

			// Extract vCPUs from capabilities.
			info.vcpus = extractVCPUs(sku.Capabilities)

			// Check restrictions.
			info.restricted, info.reason = checkRestrictions(sku.Restrictions)

			result[skuNameLower] = info

			slog.Debug("found SKU",
				"sku", *sku.Name,
				"region", region,
				"family", info.family,
				"vcpus", info.vcpus,
				"restricted", info.restricted,
			)
		}
	}

	return result, nil
}

// extractVCPUs extracts the vCPU count from SKU capabilities.
func extractVCPUs(capabilities []*armcompute.ResourceSKUCapabilities) int {
	for _, cap := range capabilities {
		if cap.Name == nil || cap.Value == nil {
			continue
		}

		if strings.EqualFold(*cap.Name, "vCPUs") {
			vcpus, err := strconv.Atoi(*cap.Value)
			if err != nil {
				slog.Warn("failed to parse vCPUs capability", "value", *cap.Value, "error", err)
				return 0
			}

			return vcpus
		}
	}

	return 0
}

// checkRestrictions checks if a SKU has any restrictions that prevent its use.
func checkRestrictions(restrictions []*armcompute.ResourceSKURestrictions) (restricted bool, reason string) {
	for _, r := range restrictions {
		if r.ReasonCode == nil {
			continue
		}

		if *r.ReasonCode == armcompute.ResourceSKURestrictionsReasonCodeNotAvailableForSubscription {
			return true, "not available for subscription"
		}
	}

	return false, ""
}

// quotaInfo holds quota data for a single family.
type quotaInfo struct {
	limit int64
	used  int64
}

// fetchQuota queries the Usage API and returns a map of family name → quota info.
func (c *Checker) fetchQuota(ctx context.Context, region string) (map[string]*quotaInfo, error) {
	result := make(map[string]*quotaInfo)

	pager := c.usageClient.NewListPager(region, nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list usage: %w", err)
		}

		for _, usage := range page.Value {
			if usage.Name == nil || usage.Name.Value == nil {
				continue
			}

			var limit int64
			var used int64

			if usage.Limit != nil {
				limit = *usage.Limit
			}

			if usage.CurrentValue != nil {
				used = int64(*usage.CurrentValue)
			}

			familyName := strings.ToLower(*usage.Name.Value)
			result[familyName] = &quotaInfo{
				limit: limit,
				used:  used,
			}
		}
	}

	slog.Debug("fetched quota data", "region", region, "families", len(result))
	return result, nil
}
