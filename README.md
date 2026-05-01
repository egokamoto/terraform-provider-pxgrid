# Terraform Provider pxgrid

Terraform provider for Proxmox bootstrap workflows, host networking, SDN lab
setup, and LXC initialization.

`pxgrid` is intentionally scoped as a day-0/day-1 bootstrap provider. It is
not a replacement for [`bpg/proxmox`](https://registry.terraform.io/providers/bpg/proxmox);
use `bpg/proxmox` for broad Proxmox lifecycle coverage and use `pxgrid` when
you need Terraform-managed bootstrap flows that combine:

- Proxmox API operations;
- controlled SSH operations on the Proxmox host;
- LXC initialization with startup scripts and files.

## Status

This repository is being prepared as the standalone provider home for
`bastet-cat/pxgrid`. The first public release is expected to be experimental
(`v0.x`) until the release pipeline, generated docs and lab validation policy
are in place.

## Planned Provider Source

```hcl
terraform {
  required_providers {
    pxgrid = {
      source  = "bastet-cat/pxgrid"
      version = "~> 0.1"
    }
  }
}
```

## Development

```bash
make build
make test
```

The provider implementation is being migrated from the reference lab
repository. Before adding releases, preserve the decisions and trade-offs
listed in [docs/decision-coverage.md](docs/decision-coverage.md).

For local Terraform development before the first Registry release, use the
development override flow in [docs/development.md](docs/development.md).

## Differentiation

`pxgrid` should be documented and released as a bootstrap-focused provider:

- bridge and SDN setup for isolated Proxmox labs;
- host NAT/firewall/key/password bootstrap where API-only flows are not enough;
- LXC bootstrap with startup files and scripts;
- reference validation through an isolated k3s lab.

For general-purpose VM, storage, HA, cluster and broad Proxmox resource
coverage, prefer `bpg/proxmox`.
