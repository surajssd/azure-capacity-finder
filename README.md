# azure-capacity-finder

A CLI tool that finds Azure regions with available VM capacity for specific SKUs. It checks SKU availability and vCPU quota across regions, and can optionally validate real physical capacity by creating (and auto-deleting) a Virtual Machine Scale Set.

## Why

Azure quota APIs can report available capacity even when a region lacks physical resources to fulfill a request. This tool solves two problems:

1. **`find`** -- Quickly scan all Azure regions for SKU availability and sufficient vCPU quota.
2. **`create`** -- Go a step further and actually provision a VMSS to confirm real capacity, then automatically clean up.

## Prerequisites

- [Go 1.25+](https://go.dev/dl/) (to build from source)
- [Azure CLI](https://learn.microsoft.com/en-us/cli/azure/install-azure-cli) (`az`) installed and logged in, or another [Azure authentication method](https://learn.microsoft.com/en-us/azure/developer/go/azure-sdk-authentication)

## Installation

```bash
go install github.com/surajssd/azure-capacity-finder@latest
```

Or build from source:

```bash
git clone https://github.com/surajssd/azure-capacity-finder.git
cd azure-capacity-finder
go build .
```

## Authentication

The tool uses Azure's [DefaultAzureCredential](https://learn.microsoft.com/en-us/azure/developer/go/azure-sdk-authentication) chain. The simplest way is:

```bash
az login
```

Other supported methods include managed identity, service principals, and environment variables (`AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, `AZURE_CLIENT_SECRET`).

### Subscription resolution

The subscription is resolved in this order:

1. `--subscriptions` flag (if provided)
2. `AZURE_SUBSCRIPTION_ID` environment variable
3. Current `az` CLI subscription (`az account show`)

## Usage

### `find` -- Check quota availability

Scan regions for SKU availability and vCPU quota without creating any resources.

```bash
azure-capacity-finder find --sku <SKU_NAME> [flags]
```

#### Flags

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--sku` | Yes | -- | Comma-separated VM SKU names |
| `--subscriptions` | No | Current subscription | Comma-separated subscription IDs |
| `--regions` | No | All Azure regions | Comma-separated region names |
| `--scale` | No | `1` | Number of VMs needed |
| `--parallelism` | No | `3` | Max concurrent region checks |

#### Examples

```bash
# Find regions with capacity for a single SKU
azure-capacity-finder find --sku Standard_NC96ads_A100_v4

# Check multiple SKUs across specific regions
azure-capacity-finder find --sku Standard_NC96ads_A100_v4,Standard_D2s_v3 --regions eastus,westus2,westeurope

# Check if there's enough quota for 4 VMs
azure-capacity-finder find --sku Standard_D2s_v3 --scale 4

# Search across multiple subscriptions
azure-capacity-finder find --sku Standard_D2s_v3 --subscriptions "sub-id-1,sub-id-2"

# Increase parallelism for faster scanning
azure-capacity-finder find --sku Standard_D2s_v3 --parallelism 10
```

#### Example output

```
✅ Found 3 region(s) with available capacity:

REGION      SUBSCRIPTION  SKU                       FAMILY                vCPUs  QUOTA FREE  QUOTA LIMIT  STATUS
------      ------------  ---                       ------                -----  ----------  -----------  ------
eastus      aaaa-bbb...   Standard_NC96ads_A100_v4  standardNCADSA100v4F  96     288         400          ✅
westeurope  dddd-eee...   Standard_NC96ads_A100_v4  standardNCADSA100v4F  96     192         300          ✅
westus2     aaaa-bbb...   Standard_NC96ads_A100_v4  standardNCADSA100v4F  96     96          100          ✅

57 other region(s): 49 had no capacity, 8 had errors. Use --verbose for details.
```

### `create` -- Validate real physical capacity

Goes beyond quota checks by actually creating a VMSS to confirm real physical capacity. The VMSS is automatically deleted after successful provisioning.

The command first runs the same quota checks as `find`, then sequentially attempts to create a VMSS in each region with available quota until one succeeds.

```bash
azure-capacity-finder create --sku <SKU_NAME> [flags]
```

#### Flags

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--sku` | Yes | -- | VM SKU name (single SKU) |
| `--subscriptions` | No | Current subscription | Comma-separated subscription IDs |
| `--regions` | No | All Azure regions | Comma-separated region names |
| `--zones` | No | Non-zonal | Comma-separated availability zones (e.g. `1,2,3`) |
| `--scale` | No | `1` | Number of VM instances in the VMSS |
| `--prefix` | No | `acf` | Prefix for all resource names (RG, VNet, VMSS) |
| `--parallelism` | No | `3` | Max concurrent region checks (quota phase only) |

> **Note:** `--zones` requires `--regions` to also be set.

#### Examples

```bash
# Validate capacity with VMSS creation (auto-discover regions)
azure-capacity-finder create --sku Standard_NC96ads_A100_v4 --scale 2

# Specify regions and availability zones
azure-capacity-finder create --sku Standard_NC96ads_A100_v4 --regions eastus,westus2 --zones 1,2

# Custom resource name prefix
azure-capacity-finder create --sku Standard_D2s_v3 --prefix mytest

# Specific region with verbose logging
azure-capacity-finder create --sku Standard_D2s_v3 --regions eastus -v
```

#### Example output

```
✅ Found 2 region(s) with available capacity:

REGION      SUBSCRIPTION  SKU                       FAMILY                vCPUs  QUOTA FREE  QUOTA LIMIT  STATUS
------      ------------  ---                       ------                -----  ----------  -----------  ------
eastus      aaaa-bbb...   Standard_NC96ads_A100_v4  standardNCADSA100v4F  96     288         400          ✅
westeurope  dddd-eee...   Standard_NC96ads_A100_v4  standardNCADSA100v4F  96     192         300          ✅

58 other region(s): 50 had no capacity, 8 had errors. Use --verbose for details.

🚀 Attempting VMSS creation in eastus (aaaa-bbbb-...)...
   Creating resource group acf-eastus-a1b2c3d4...
   Creating virtual network...
   Creating VMSS (Standard_NC96ads_A100_v4 × 2)...
   ❌ AllocationFailed: no physical capacity in region (VMSSCreation)
   Deleting resource group acf-eastus-a1b2c3d4...
   Resource group deleted.

🚀 Attempting VMSS creation in westeurope (dddd-eeee-...)...
   Creating resource group acf-westeurope-f5e6d7c8...
   Creating virtual network...
   Creating VMSS (Standard_NC96ads_A100_v4 × 2)...
   VMSS provisioned successfully.
   Deleting resource group acf-westeurope-f5e6d7c8...
   Resource group deleted.
✅ Capacity validated in westeurope! VMSS provisioned and cleaned up.
```

#### What gets created

The `create` command provisions the following resources in a dedicated resource group, then deletes the entire group:

```
Resource Group: <prefix>-<region>-<random>
├── VNet: <prefix>-vnet (10.0.0.0/16)
│   └── Subnet: <prefix>-subnet (10.0.0.0/24)
└── VMSS: <prefix>-vmss
```

All resources are tagged with `created-by: azure-capacity-finder` and `purpose: capacity-probe`.

#### Cleanup

The resource group is always deleted when the command finishes, whether the VMSS creation succeeded or failed. If you interrupt the command with Ctrl+C, cleanup still runs. If cleanup itself fails, you'll see a warning with a manual delete command:

```
⚠️  Failed to delete resource group 'acf-eastus-a1b2c3d4'. Delete manually:
  az group delete --name acf-eastus-a1b2c3d4 --subscription aaaa-bbbb-cccc-dddd --yes --no-wait
```

## Global Flags

| Flag | Description |
|------|-------------|
| `-v`, `--verbose` | Enable debug logging (printed to stderr) |
| `-h`, `--help` | Show help |

## How it works

1. **Authenticate** with Azure using `DefaultAzureCredential`.
2. **Resolve subscriptions** from flag, environment variable, or `az` CLI.
3. **Check SKU availability** using the [Resource SKUs API](https://learn.microsoft.com/en-us/rest/api/compute/resource-skus/list) -- verifies the SKU exists in the region and is not restricted.
4. **Check vCPU quota** using the [Usage API](https://learn.microsoft.com/en-us/rest/api/compute/usage/list) -- verifies enough free quota in the SKU's vCPU family.
5. **(create only)** **Provision a VMSS** to validate real physical capacity, then delete the resource group.
