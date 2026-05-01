# Differentiation

`pxgrid` is a bootstrap-focused Terraform provider for Proxmox VE. It is
designed for day-0/day-1 workflows where a Proxmox host must become a usable
foundation for isolated labs, small clusters or edge environments.

## Use `pxgrid` For

- host networking bootstrap where the Proxmox API must be combined with
  controlled host SSH;
- isolated bridge, NAT and firewall setup;
- LXC creation with startup scripts and file injection;
- repeatable lab foundations such as an isolated k3s environment;
- operational bootstrap flows that should be codified instead of kept as shell
  notes.

## Use `bpg/proxmox` For

- broad Proxmox lifecycle coverage;
- general VM lifecycle management;
- storage, HA, cluster and node resources outside the `pxgrid` bootstrap
  niche;
- production estates that need a mature general-purpose Proxmox provider.

## Use Both When

Use `bpg/proxmox` for broad infrastructure objects and `pxgrid` for the
bootstrap layer that needs host networking, NAT, firewall or LXC initialization
semantics. Keep ownership boundaries explicit in module docs.

## Known Limits

- `pxgrid` is not intended to replace broad Proxmox providers.
- Some resources require SSH access to the Proxmox host.
- Some confidence checks are lab acceptance checks, not unit tests, because
  they depend on real Proxmox, Cloudflare or k3s behavior.
- Early releases are expected to be experimental until release signing,
  generated docs and lab acceptance policies mature.
