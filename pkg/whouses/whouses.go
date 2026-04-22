package whouses

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	armcompute "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v6"
)

// aksRGPattern matches AKS managed resource groups: MC_{resourceGroup}_{clusterName}_{location}
var aksRGPattern = regexp.MustCompile(`^MC_([^_]+)_([^_]+)_.+$`)

// Run searches for VMs and VMSSs using the specified SKU in the given subscription.
func Run(ctx context.Context, cred azcore.TokenCredential, subscriptionID, skuName string, refresh bool) (*Result, error) {
	slog.Info("searching for resources using SKU", "sku", skuName, "subscription", subscriptionID)

	vms, err := findVMs(ctx, cred, subscriptionID, skuName, refresh)
	if err != nil {
		return nil, fmt.Errorf("listing VMs: %w", err)
	}

	vmssList, err := findVMSSs(ctx, cred, subscriptionID, skuName, refresh)
	if err != nil {
		return nil, fmt.Errorf("listing VMSSs: %w", err)
	}

	return &Result{
		VMs:            vms,
		VMSSs:          vmssList,
		SubscriptionID: subscriptionID,
	}, nil
}

func findVMs(ctx context.Context, cred azcore.TokenCredential, subscriptionID, skuName string, refresh bool) ([]VMResult, error) {
	type cachedVM struct {
		Name          string `json:"name"`
		ResourceGroup string `json:"resourceGroup"`
		Location      string `json:"location"`
		VMSize        string `json:"vmSize"`
	}

	allVMs, err := loadOrFetch(fmt.Sprintf("all_vms_%s.json", subscriptionID), refresh, func() ([]cachedVM, error) {
		slog.Info("fetching all VMs...")

		client, err := armcompute.NewVirtualMachinesClient(subscriptionID, cred, nil)
		if err != nil {
			return nil, fmt.Errorf("creating VM client: %w", err)
		}

		var vms []cachedVM

		pager := client.NewListAllPager(nil)
		for pager.More() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing VMs: %w", err)
			}

			for _, vm := range page.Value {
				var vmSize, name, location, rg string

				if vm.Properties != nil && vm.Properties.HardwareProfile != nil && vm.Properties.HardwareProfile.VMSize != nil {
					vmSize = string(*vm.Properties.HardwareProfile.VMSize)
				}

				if vm.Name != nil {
					name = *vm.Name
				}

				if vm.Location != nil {
					location = *vm.Location
				}

				if vm.ID != nil {
					rg = extractResourceGroup(*vm.ID)
				}

				vms = append(vms, cachedVM{
					Name:          name,
					ResourceGroup: rg,
					Location:      location,
					VMSize:        vmSize,
				})
			}
		}

		return vms, nil
	})
	if err != nil {
		return nil, err
	}

	// Filter by SKU.
	var results []VMResult

	for _, vm := range allVMs {
		if strings.EqualFold(vm.VMSize, skuName) {
			results = append(results, VMResult{
				Name:          vm.Name,
				ResourceGroup: vm.ResourceGroup,
				Location:      vm.Location,
			})
		}
	}

	return results, nil
}

func findVMSSs(ctx context.Context, cred azcore.TokenCredential, subscriptionID, skuName string, refresh bool) ([]VMSSResult, error) {
	type cachedVMSS struct {
		Name          string `json:"name"`
		ResourceGroup string `json:"resourceGroup"`
		Location      string `json:"location"`
		SKUName       string `json:"skuName"`
		Capacity      int64  `json:"capacity"`
	}

	allVMSSs, err := loadOrFetch(fmt.Sprintf("all_vmss_%s.json", subscriptionID), refresh, func() ([]cachedVMSS, error) {
		slog.Info("fetching all VMSSs...")

		client, err := armcompute.NewVirtualMachineScaleSetsClient(subscriptionID, cred, nil)
		if err != nil {
			return nil, fmt.Errorf("creating VMSS client: %w", err)
		}

		var vmssList []cachedVMSS

		pager := client.NewListAllPager(nil)
		for pager.More() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing VMSSs: %w", err)
			}

			for _, vmss := range page.Value {
				var name, location, rg, sku string
				var cap int64

				if vmss.Name != nil {
					name = *vmss.Name
				}

				if vmss.Location != nil {
					location = *vmss.Location
				}

				if vmss.ID != nil {
					rg = extractResourceGroup(*vmss.ID)
				}

				if vmss.SKU != nil {
					if vmss.SKU.Name != nil {
						sku = *vmss.SKU.Name
					}

					if vmss.SKU.Capacity != nil {
						cap = *vmss.SKU.Capacity
					}
				}

				vmssList = append(vmssList, cachedVMSS{
					Name:          name,
					ResourceGroup: rg,
					Location:      location,
					SKUName:       sku,
					Capacity:      cap,
				})
			}
		}

		return vmssList, nil
	})
	if err != nil {
		return nil, err
	}

	// Filter by SKU and detect AKS clusters.
	var results []VMSSResult

	for _, vmss := range allVMSSs {
		if !strings.EqualFold(vmss.SKUName, skuName) {
			continue
		}

		result := VMSSResult{
			Name:          vmss.Name,
			ResourceGroup: vmss.ResourceGroup,
			Location:      vmss.Location,
			Capacity:      vmss.Capacity,
		}

		// Detect AKS managed resource group.
		if matches := aksRGPattern.FindStringSubmatch(vmss.ResourceGroup); matches != nil {
			result.AKSResourceGroup = matches[1]
			result.AKSClusterName = matches[2]
		}

		results = append(results, result)
	}

	return results, nil
}

// extractResourceGroup extracts the resource group name from an Azure resource ID.
func extractResourceGroup(resourceID string) string {
	parts := strings.Split(resourceID, "/")

	for i, p := range parts {
		if strings.EqualFold(p, "resourceGroups") && i+1 < len(parts) {
			return parts[i+1]
		}
	}

	return ""
}
