package whouses

import (
	"fmt"
	"os"
	"text/tabwriter"
)

// PrintResults prints the who-uses results as formatted tables.
func PrintResults(result *Result) {
	fmt.Println()

	// Print VMs.
	if len(result.VMs) > 0 {
		fmt.Printf("Virtual Machines using the requested SKU:\n\n")

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "VM NAME\tRESOURCE GROUP\tLOCATION")
		fmt.Fprintln(w, "-------\t--------------\t--------")

		for _, vm := range result.VMs {
			fmt.Fprintf(w, "%s\t%s\t%s\n", vm.Name, vm.ResourceGroup, vm.Location)
		}

		w.Flush()
		fmt.Println()
	} else {
		fmt.Println("No VMs found using the requested SKU.")
		fmt.Println()
	}

	// Print VMSSs.
	if len(result.VMSSs) > 0 {
		fmt.Printf("Virtual Machine Scale Sets using the requested SKU:\n\n")

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "VMSS NAME\tRESOURCE GROUP\tLOCATION\tCAPACITY")
		fmt.Fprintln(w, "---------\t--------------\t--------\t--------")

		for _, vmss := range result.VMSSs {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", vmss.Name, vmss.ResourceGroup, vmss.Location, vmss.Capacity)
		}

		w.Flush()
		fmt.Println()

		// Print AKS cluster links.
		fmt.Println("AKS Cluster Links:")

		aksFound := false

		for _, vmss := range result.VMSSs {
			if vmss.AKSClusterName == "" {
				continue
			}

			aksFound = true
			portalURL := fmt.Sprintf(
				"https://portal.azure.com/#@/resource/subscriptions/%s/resourceGroups/%s/providers/Microsoft.ContainerService/managedClusters/%s/overview",
				result.SubscriptionID,
				vmss.AKSResourceGroup,
				vmss.AKSClusterName,
			)

			fmt.Printf("  %s (in resource group %s):\n", vmss.AKSClusterName, vmss.AKSResourceGroup)
			fmt.Printf("    %s\n", portalURL)
		}

		if !aksFound {
			fmt.Println("  No AKS-managed resource groups detected.")
		}

		fmt.Println()
	} else {
		fmt.Println("No VMSSs found using the requested SKU.")
		fmt.Println()
	}

	fmt.Println("✅ Search complete!")
}
