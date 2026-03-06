package azure

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

// ResolveSubscriptions determines which Azure subscription IDs to use.
// Fallback chain: provided list → AZURE_SUBSCRIPTION_ID env var → az account show.
func ResolveSubscriptions(provided []string) ([]string, error) {
	// If subscriptions were explicitly provided, use them.
	if len(provided) > 0 {
		slog.Debug("using provided subscription IDs", "subscriptions", provided)
		return provided, nil
	}

	// Try the AZURE_SUBSCRIPTION_ID environment variable.
	if envSub := os.Getenv("AZURE_SUBSCRIPTION_ID"); envSub != "" {
		slog.Debug("using subscription from AZURE_SUBSCRIPTION_ID", "subscription", envSub)
		return []string{envSub}, nil
	}

	// Fall back to az CLI.
	slog.Debug("resolving subscription from az CLI")
	sub, err := getSubscriptionFromCLI()
	if err != nil {
		return nil, fmt.Errorf("no subscription provided and failed to get default from az CLI: %w", err)
	}

	slog.Debug("using subscription from az CLI", "subscription", sub)
	return []string{sub}, nil
}

func getSubscriptionFromCLI() (string, error) {
	cmd := exec.Command("az", "account", "show", "--query", "id", "-o", "tsv")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("az account show failed: %w", err)
	}

	sub := strings.TrimSpace(string(out))
	if sub == "" {
		return "", fmt.Errorf("az account show returned empty subscription ID")
	}

	return sub, nil
}
