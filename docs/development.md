# Development

Use a Terraform CLI development override while the provider is not published
to the Registry.

Build the provider:

```bash
make build
```

Create a local CLI config file:

```hcl
provider_installation {
  dev_overrides {
    "bastet-cat/pxgrid" = "/absolute/path/to/terraform-provider-pxgrid/dist/linux_amd64"
  }
  direct {}
}
```

Run Terraform with that config. When using `dev_overrides`, skip
`terraform init` until the provider is published in the Registry:

```bash
TF_CLI_CONFIG_FILE=/path/to/dev.tfrc terraform -chdir=examples/minimal plan
```

The provider binary must be named `terraform-provider-pxgrid`.
