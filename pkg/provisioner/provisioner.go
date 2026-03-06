package provisioner

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v5"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"golang.org/x/crypto/ssh"
)

// allocationFailureCodes are Azure error codes indicating no physical capacity.
var allocationFailureCodes = map[string]bool{
	"AllocationFailed":                        true,
	"OverconstrainedAllocationRequest":        true,
	"ZonalAllocationFailed":                   true,
	"OverconstrainedZonalAllocationRequest":   true,
}

// quotaFailureCodes are Azure error codes indicating quota issues.
var quotaFailureCodes = map[string]bool{
	"OperationNotAllowed":  true,
	"QuotaExceeded":        true,
	"ResourceQuotaExceeded": true,
}

// skuFailureCodes are Azure error codes indicating the SKU is unavailable.
var skuFailureCodes = map[string]bool{
	"SkuNotAvailable": true,
}

// ProvisionError represents a classified provisioning error.
type ProvisionError struct {
	Code    string
	Message string
	// Retryable indicates whether the caller should try the next region.
	Retryable bool
}

func (e *ProvisionError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Provisioner orchestrates VMSS creation for capacity validation.
type Provisioner struct {
	subscriptionID string
	prefix         string
	rgClient       *armresources.ResourceGroupsClient
	vnetClient     *armnetwork.VirtualNetworksClient
	vmssClient     *armcompute.VirtualMachineScaleSetsClient
}

// NewProvisioner creates a new Provisioner for the given subscription.
func NewProvisioner(subscriptionID, prefix string, cred azcore.TokenCredential) (*Provisioner, error) {
	rgClient, err := armresources.NewResourceGroupsClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating resource groups client: %w", err)
	}

	vnetClient, err := armnetwork.NewVirtualNetworksClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating virtual networks client: %w", err)
	}

	vmssClient, err := armcompute.NewVirtualMachineScaleSetsClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating VMSS client: %w", err)
	}

	return &Provisioner{
		subscriptionID: subscriptionID,
		prefix:         prefix,
		rgClient:       rgClient,
		vnetClient:     vnetClient,
		vmssClient:     vmssClient,
	}, nil
}

// Provision creates a VMSS in the given region to validate capacity,
// then deletes the resource group. It returns a ProvisionError on failure.
func (p *Provisioner) Provision(ctx context.Context, region, skuName string, scale int, zones []string) error {
	names, err := GenerateNames(p.prefix, region)
	if err != nil {
		return &ProvisionError{Code: "NameGeneration", Message: err.Error(), Retryable: true}
	}

	fmt.Printf("\n🚀 Attempting VMSS creation in %s (%s)...\n", region, truncateSub(p.subscriptionID))

	// Always clean up the resource group when we're done.
	rgCreated := false

	defer func() {
		if !rgCreated {
			return
		}
		p.deleteResourceGroup(names.ResourceGroup)
	}()

	// Step 1: Create resource group.
	fmt.Printf("   Creating resource group %s...\n", names.ResourceGroup)

	if err := p.createResourceGroup(ctx, names.ResourceGroup, region); err != nil {
		return classifyError("ResourceGroupCreation", err)
	}

	rgCreated = true

	// Step 2: Create VNet with inline subnet.
	fmt.Printf("   Creating virtual network...\n")

	subnetID, err := p.createVNet(ctx, names, region)
	if err != nil {
		return classifyError("VNetCreation", err)
	}

	// Step 3: Generate throwaway SSH key.
	pubKeyStr, err := generateSSHKey()
	if err != nil {
		return &ProvisionError{Code: "SSHKeyGeneration", Message: err.Error(), Retryable: true}
	}

	// Step 4: Create VMSS.
	fmt.Printf("   Creating VMSS (%s × %d)...\n", skuName, scale)

	if err := p.createVMSS(ctx, names, region, skuName, scale, zones, subnetID, pubKeyStr); err != nil {
		provErr := classifyError("VMSSCreation", err)
		var pe *ProvisionError
		if errors.As(provErr, &pe) {
			fmt.Printf("   ❌ %s\n", pe.Error())
		}
		return provErr
	}

	fmt.Printf("   VMSS provisioned successfully.\n")

	return nil
}

// createResourceGroup creates a resource group in the given region.
func (p *Provisioner) createResourceGroup(ctx context.Context, name, region string) error {
	_, err := p.rgClient.CreateOrUpdate(ctx, name, armresources.ResourceGroup{
		Location: to.Ptr(region),
		Tags: map[string]*string{
			"created-by": to.Ptr("azure-capacity-finder"),
			"purpose":    to.Ptr("capacity-probe"),
		},
	}, nil)
	return err
}

// deleteResourceGroup deletes a resource group using a background context
// so that cleanup proceeds even if the original context is cancelled (e.g. Ctrl+C).
func (p *Provisioner) deleteResourceGroup(name string) {
	fmt.Printf("   Deleting resource group %s...\n", name)

	//nolint:lostcancel // We intentionally don't cancel this context — cleanup must complete.
	ctx := context.Background()

	poller, err := p.rgClient.BeginDelete(ctx, name, &armresources.ResourceGroupsClientBeginDeleteOptions{
		ForceDeletionTypes: to.Ptr("Microsoft.Compute/virtualMachines,Microsoft.Compute/virtualMachineScaleSets"),
	})
	if err != nil {
		printCleanupWarning(name, p.subscriptionID)
		return
	}

	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		printCleanupWarning(name, p.subscriptionID)
		return
	}

	fmt.Printf("   Resource group deleted.\n")
}

// createVNet creates a virtual network with an inline subnet and returns the subnet's ARM resource ID.
func (p *Provisioner) createVNet(ctx context.Context, names ResourceNames, region string) (string, error) {
	poller, err := p.vnetClient.BeginCreateOrUpdate(ctx, names.ResourceGroup, names.VNet, armnetwork.VirtualNetwork{
		Location: to.Ptr(region),
		Properties: &armnetwork.VirtualNetworkPropertiesFormat{
			AddressSpace: &armnetwork.AddressSpace{
				AddressPrefixes: []*string{to.Ptr("10.0.0.0/16")},
			},
			Subnets: []*armnetwork.Subnet{
				{
					Name: to.Ptr(names.Subnet),
					Properties: &armnetwork.SubnetPropertiesFormat{
						AddressPrefix: to.Ptr("10.0.0.0/24"),
					},
				},
			},
		},
	}, nil)
	if err != nil {
		return "", err
	}

	resp, err := poller.PollUntilDone(ctx, nil)
	if err != nil {
		return "", err
	}

	// Extract subnet ID from the response.
	if resp.Properties != nil && len(resp.Properties.Subnets) > 0 && resp.Properties.Subnets[0].ID != nil {
		return *resp.Properties.Subnets[0].ID, nil
	}

	// Construct it manually as a fallback.
	subnetID := fmt.Sprintf(
		"/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/virtualNetworks/%s/subnets/%s",
		p.subscriptionID, names.ResourceGroup, names.VNet, names.Subnet,
	)

	return subnetID, nil
}

// createVMSS creates a Virtual Machine Scale Set.
func (p *Provisioner) createVMSS(
	ctx context.Context,
	names ResourceNames,
	region, skuName string,
	scale int,
	zones []string,
	subnetID, sshPubKey string,
) error {
	// Build zones parameter.
	var zonesPtrs []*string
	for _, z := range zones {
		zonesPtrs = append(zonesPtrs, to.Ptr(z))
	}

	vmss := armcompute.VirtualMachineScaleSet{
		Location: to.Ptr(region),
		Zones:    zonesPtrs,
		SKU: &armcompute.SKU{
			Name:     to.Ptr(skuName),
			Capacity: to.Ptr(int64(scale)),
			Tier:     to.Ptr("Standard"),
		},
		Properties: &armcompute.VirtualMachineScaleSetProperties{
			Overprovision:        to.Ptr(false),
			SinglePlacementGroup: to.Ptr(false),
			UpgradePolicy: &armcompute.UpgradePolicy{
				Mode: to.Ptr(armcompute.UpgradeModeManual),
			},
			VirtualMachineProfile: &armcompute.VirtualMachineScaleSetVMProfile{
				OSProfile: &armcompute.VirtualMachineScaleSetOSProfile{
					ComputerNamePrefix: to.Ptr(p.prefix),
					AdminUsername:      to.Ptr("acfadmin"),
					LinuxConfiguration: &armcompute.LinuxConfiguration{
						DisablePasswordAuthentication: to.Ptr(true),
						SSH: &armcompute.SSHConfiguration{
							PublicKeys: []*armcompute.SSHPublicKey{
								{
									Path:    to.Ptr("/home/acfadmin/.ssh/authorized_keys"),
									KeyData: to.Ptr(sshPubKey),
								},
							},
						},
					},
				},
				StorageProfile: &armcompute.VirtualMachineScaleSetStorageProfile{
					ImageReference: &armcompute.ImageReference{
						Publisher: to.Ptr("Canonical"),
						Offer:    to.Ptr("ubuntu-24_04-lts"),
						SKU:      to.Ptr("server"),
						Version:  to.Ptr("latest"),
					},
					OSDisk: &armcompute.VirtualMachineScaleSetOSDisk{
						CreateOption: to.Ptr(armcompute.DiskCreateOptionTypesFromImage),
						ManagedDisk: &armcompute.VirtualMachineScaleSetManagedDiskParameters{
							StorageAccountType: to.Ptr(armcompute.StorageAccountTypesStandardLRS),
						},
					},
				},
				NetworkProfile: &armcompute.VirtualMachineScaleSetNetworkProfile{
					NetworkInterfaceConfigurations: []*armcompute.VirtualMachineScaleSetNetworkConfiguration{
						{
							Name: to.Ptr(fmt.Sprintf("%s-nic", p.prefix)),
							Properties: &armcompute.VirtualMachineScaleSetNetworkConfigurationProperties{
								Primary: to.Ptr(true),
								IPConfigurations: []*armcompute.VirtualMachineScaleSetIPConfiguration{
									{
										Name: to.Ptr(fmt.Sprintf("%s-ipconfig", p.prefix)),
										Properties: &armcompute.VirtualMachineScaleSetIPConfigurationProperties{
											Subnet: &armcompute.APIEntityReference{
												ID: to.Ptr(subnetID),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	poller, err := p.vmssClient.BeginCreateOrUpdate(ctx, names.ResourceGroup, names.VMSS, vmss, nil)
	if err != nil {
		return err
	}

	_, err = poller.PollUntilDone(ctx, nil)
	return err
}

// generateSSHKey generates a throwaway Ed25519 SSH key pair and returns the public key
// in OpenSSH authorized_keys format. The private key is discarded.
func generateSSHKey() (string, error) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generating ed25519 key: %w", err)
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("converting to SSH public key: %w", err)
	}

	// MarshalAuthorizedKey returns the key in "ssh-ed25519 AAAA..." format with a trailing newline.
	authorizedKey := string(ssh.MarshalAuthorizedKey(sshPub))

	return authorizedKey, nil
}

// classifyError examines an Azure API error and returns a classified ProvisionError.
func classifyError(phase string, err error) *ProvisionError {
	var respErr *azcore.ResponseError
	if !errors.As(err, &respErr) {
		slog.Debug("unclassified error", "phase", phase, "error", err)
		return &ProvisionError{
			Code:      "Unknown",
			Message:   err.Error(),
			Retryable: true,
		}
	}

	code := respErr.ErrorCode

	switch {
	case allocationFailureCodes[code]:
		return &ProvisionError{
			Code:      code,
			Message:   fmt.Sprintf("no physical capacity in region (%s)", phase),
			Retryable: true,
		}
	case quotaFailureCodes[code]:
		return &ProvisionError{
			Code:      code,
			Message:   fmt.Sprintf("quota exceeded (%s)", phase),
			Retryable: true,
		}
	case skuFailureCodes[code]:
		return &ProvisionError{
			Code:      code,
			Message:   fmt.Sprintf("SKU not available in region (%s)", phase),
			Retryable: true,
		}
	default:
		slog.Debug("unrecognized Azure error code", "phase", phase, "code", code, "message", respErr.Error())
		return &ProvisionError{
			Code:      code,
			Message:   respErr.Error(),
			Retryable: true,
		}
	}
}

// printCleanupWarning prints a warning when resource group deletion fails.
func printCleanupWarning(rgName, subscriptionID string) {
	fmt.Printf("⚠️  Failed to delete resource group '%s'. Delete manually:\n", rgName)
	fmt.Printf("  az group delete --name %s --subscription %s --yes --no-wait\n", rgName, subscriptionID)
}

// truncateSub shortens a subscription ID for display.
func truncateSub(sub string) string {
	if len(sub) > 12 {
		return sub[:8] + "..."
	}
	return sub
}

