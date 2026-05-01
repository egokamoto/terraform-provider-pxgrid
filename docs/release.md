# Release Checklist

The first public release is not ready until these gates are satisfied:

- Provider code has been migrated from the reference lab repository.
- `go test ./...` passes.
- Generated provider docs exist.
- The decision coverage matrix has been reconciled.
- GoReleaser creates multi-platform provider archives.
- Release checksums are signed.
- A minimal external Terraform project can run `terraform init` with
  `source = "bastet-cat/pxgrid"`.

Before the first Registry release, local development should validate
`source = "bastet-cat/pxgrid"` with Terraform CLI `dev_overrides` and
`terraform plan`; `terraform init` is expected to query the Registry and fail
until a public version exists.
