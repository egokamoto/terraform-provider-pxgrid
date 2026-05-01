# Trade-offs and Operational Notes

This page captures behavior that is important for operators but is not always
visible from generated schema documentation.

## Host SSH Is Intentional

Some workflows are not fully covered by the Proxmox API. `pxgrid` therefore
supports controlled SSH operations for host NAT, host firewall, authorized keys,
password bootstrap and LXC startup flows.

Use API-only resources when possible. Use SSH-backed resources when the
bootstrap problem requires host-level changes.

## Network Reload Uses the Proxmox API

`pxgrid_network_bridge` applies network changes through the Proxmox network
reload API, not shell `ifreload -a`. The provider waits on the returned task
when Proxmox returns a `UPID`, and bridge visibility must be confirmed before
the resource is treated as ready.

Some Proxmox versions may show a bridge through the per-interface endpoint
before the network list endpoint includes it. The provider checks both paths.
Missing interfaces can be reported as `404` or as a `400` response containing
`interface does not exist`; both mean absent.

## Container Lifecycle Waits Are Conservative

Replacing an LXC with the same `vmid` is sensitive to Proxmox task timing and
`pmxcfs` propagation. The provider waits for absence after destroy and presence
after create before continuing with startup scripts or host access.

This can make applies slower, but it avoids false success and partial recovery
states.

## Template Downloads Use `download-url`

The provider uses the Proxmox `download-url` endpoint and follows the returned
task when a `UPID` is returned. Listing storage content successfully does not
prove that the token can download templates. Permission failures from
`download-url` should be treated as ACL issues first.

## Generated Files and State Can Contain Secrets

Terraform state, `.tfvars`, generated startup scripts and generated service
files can contain tokens, passwords or API keys. Keep those files ignored and
out of public commits.

## Lab-specific Decisions

The isolated k3s lab intentionally keeps the Kubernetes API private by default.
Remote administration should terminate on the administrative LXC, not directly
on the k3s API. Cloudflare Access SSH, if used, protects the public hostname
while the tunnel routes to a private SSH target.
