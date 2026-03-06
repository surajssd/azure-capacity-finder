package capacity

// CheckInput holds all parameters for a capacity check run.
type CheckInput struct {
	Subscriptions []string
	VMSKUs        []string
	Regions       []string
	Scale         int
	Parallelism   int
}

// RegionResult holds the capacity check results for a single region + subscription.
type RegionResult struct {
	Subscription string
	Region       string
	SKUs         []SKUResult
	Error        error
}

// HasCapacity returns true if all requested SKUs are available in this region.
func (r *RegionResult) HasCapacity() bool {
	if r.Error != nil {
		return false
	}

	if len(r.SKUs) == 0 {
		return false
	}

	for _, sku := range r.SKUs {
		if !sku.Available {
			return false
		}
	}

	return true
}

// SKUResult holds the availability and quota details for a single SKU in a region.
type SKUResult struct {
	SKUName    string
	Available  bool
	Reason     string
	Family     string
	VCPUs      int
	QuotaLimit int64
	QuotaUsed  int64
	QuotaNeeded int64
	QuotaFree  int64
}
