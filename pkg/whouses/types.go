package whouses

// VMResult represents a VM that matches the requested SKU.
type VMResult struct {
	Name          string
	ResourceGroup string
	Location      string
}

// VMSSResult represents a VMSS that matches the requested SKU.
type VMSSResult struct {
	Name             string
	ResourceGroup    string
	Location         string
	Capacity         int64
	AKSClusterName   string
	AKSResourceGroup string
}

// Result holds the combined results of the who-uses search.
type Result struct {
	VMs            []VMResult
	VMSSs          []VMSSResult
	SubscriptionID string
}
