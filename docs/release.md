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

## GitHub Release Pipeline

Tags matching `v*` run `.github/workflows/release.yml`. The workflow imports a
GPG private key, runs GoReleaser v2, creates platform zip files, creates
`terraform-provider-pxgrid_<version>_SHA256SUMS`, signs that checksum file, and
uploads the Terraform Registry manifest.

Required repository secrets:

- `GPG_PRIVATE_KEY`: armored private key used to sign release checksums.
- `GPG_PASSPHRASE`: passphrase for the private key.

GoReleaser intentionally creates draft GitHub releases. Review artifacts and
release notes before publishing the draft and registering or refreshing the
provider in the Terraform Registry.

## Local Dry Run

The release pipeline can be checked without publishing:

```bash
goreleaser check
goreleaser release --snapshot --clean --skip=publish --skip=sign
```

Before the first Registry release, local development should validate
`source = "bastet-cat/pxgrid"` with Terraform CLI `dev_overrides` and
`terraform plan`; `terraform init` is expected to query the Registry and fail
until a public version exists.

## First Experimental Registry Release

The first public release should be tagged as `v0.1.0` after the local gates pass.
After the GitHub release exists and the draft is published, connect the public
repository to the Terraform Registry under namespace `bastet-cat` and provider
name `pxgrid`.

The Registry publish step is intentionally manual for the first release because
it depends on namespace ownership, GitHub authorization, and GPG key
registration outside this repository.
