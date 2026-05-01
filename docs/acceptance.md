# Acceptance and Lab Validation

Automated tests cover deterministic provider behavior. Some provider confidence
still comes from the reference Proxmox lab because host networking, LXC
bootstrap, Cloudflare Access and k3s behavior require real infrastructure.

## Automated Gate

Run in this provider repository:

```bash
make test
make build
make docs
```

## Local Provider Gate

Build the provider and use Terraform CLI development overrides:

```bash
make build
TF_CLI_CONFIG_FILE=/path/to/dev.tfrc terraform -chdir=/path/to/minimal plan
```

## Reference Lab Gate

Run in the reference lab repository after updating its provider source or local
override:

```bash
make plan TF_DIR=examples/setup-isolated-k3s
make plan TF_DIR=examples/setup-isolated-k3s/cloudflare
```

Destructive checks, such as replacing containers with the same `vmid`, must be
run manually and intentionally. Do not include them in default CI.

## Manual Release Checklist

- Bridge create confirms via API.
- Bridge delete confirms absence before state removal.
- Template download follows returned `UPID`.
- LXC replacement with the same `vmid` does not require a second apply.
- Host NAT/firewall resources fail clearly when SSH credentials are absent.
- Internal-only LXCs have the required proxy environment during bootstrap.
- k3s-in-LXC bootstrap keeps `/dev/kmsg` and user namespace kubelet settings.
- Terraform state, `.tfvars` and generated files remain ignored.
