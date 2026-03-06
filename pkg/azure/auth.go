package azure

import (
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// NewCredential creates a new Azure credential using DefaultAzureCredential.
// This supports az login sessions, managed identity, and other standard methods.
func NewCredential() (azcore.TokenCredential, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure credential (have you run 'az login'?): %w", err)
	}

	return cred, nil
}
