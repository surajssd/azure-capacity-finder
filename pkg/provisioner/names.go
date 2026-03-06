package provisioner

import (
	"crypto/rand"
	"fmt"
)

// ResourceNames holds all generated resource names for a provisioning run.
type ResourceNames struct {
	ResourceGroup string // e.g. "acf-eastus-a1b2c3d4"
	VNet          string // e.g. "acf-vnet"
	Subnet        string // e.g. "acf-subnet"
	VMSS          string // e.g. "acf-vmss"
}

// GenerateNames creates unique resource names for a given prefix and region.
// The resource group gets a random hex suffix to avoid collisions.
// VNet, Subnet, and VMSS names are scoped to the RG, so they don't need random suffixes.
func GenerateNames(prefix, region string) (ResourceNames, error) {
	suffix, err := randomHex(4)
	if err != nil {
		return ResourceNames{}, fmt.Errorf("generating random suffix: %w", err)
	}

	return ResourceNames{
		ResourceGroup: fmt.Sprintf("%s-%s-%s", prefix, region, suffix),
		VNet:          fmt.Sprintf("%s-vnet", prefix),
		Subnet:        fmt.Sprintf("%s-subnet", prefix),
		VMSS:          fmt.Sprintf("%s-vmss", prefix),
	}, nil
}

// randomHex generates a random hex string of the given number of bytes.
// For example, randomHex(4) returns an 8-character hex string.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", b), nil
}
