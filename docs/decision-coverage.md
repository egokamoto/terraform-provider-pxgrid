# Decision Coverage

This repository must preserve the behavior and operational trade-offs learned
in the reference lab before publishing provider releases.

Source of truth during migration:

```text
.specs/features/pxgrid-provider-productization/decision-coverage.md
```

Before the first public release, every decision from that matrix must be
assigned to one of:

- automated provider test;
- manual lab acceptance validation;
- documentation-only coverage for lab-specific integration behavior.
