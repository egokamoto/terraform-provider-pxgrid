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
