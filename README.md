# Terraform Provider for HubSpot

Manage your HubSpot portal configuration as code: CRM properties, property
groups, pipelines, and custom object schemas. This provider targets the
HubSpot **configuration plane** — the structural setup of your portal — not
CRM records (contacts, companies, deals) themselves.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0 or [OpenTofu](https://opentofu.org/docs/intro/install/) >= 1.6
- [Go](https://golang.org/doc/install) 1.26 (to build the provider plugin)

## Using the provider

```terraform
terraform {
  required_providers {
    hubspot = {
      source = "revosai/hubspot"
    }
  }
}

provider "hubspot" {
  # Authentication uses a HubSpot private app access token,
  # read from the HUBSPOT_ACCESS_TOKEN environment variable.
}
```

Create a [private app](https://developers.hubspot.com/docs/guides/apps/private-apps/overview)
in your HubSpot portal with only the configuration scopes you need (e.g.
`crm.schemas.*`), and export its token:

```shell
export HUBSPOT_ACCESS_TOKEN="pat-..."
```

Generated documentation lives in `docs/` and on the Terraform Registry once
published.

## Developing the provider

Build, lint, and test with the included `GNUmakefile`:

```shell
make build    # go build ./...
make test     # unit tests
make testacc  # acceptance tests (TF_ACC=1)
```

Acceptance tests are **hermetic by default** — they run against a local fake
HubSpot API, so no credentials or real portal are required.

To generate or update documentation, run `make generate` (requires
[tfplugindocs](https://github.com/hashicorp/terraform-plugin-docs); see
`tools/tools.go`).

Design notes and research live in [`dev-docs/`](./dev-docs/). Contributions
are welcome — see `CONTRIBUTING.md` for guidelines.

## License

This project is licensed under the [Mozilla Public License 2.0](./LICENSE).
