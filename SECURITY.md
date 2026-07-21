# Security Policy

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues,
discussions, or pull requests.**

Instead, report them privately through GitHub's
[**private vulnerability reporting**](https://github.com/revosai/terraform-provider-hubspot/security/advisories/new)
(the **Report a vulnerability** button on the repository's **Security** tab).
This creates a private advisory visible only to you and the maintainers. If you
cannot use that channel, open a public issue that contains **no details** and
simply asks for a private contact address.

Please include as much of the following as you can:

- A description of the vulnerability and its impact
- The affected provider version(s) and the Terraform/OpenTofu version
- Steps to reproduce, ideally with a minimal Terraform configuration
- Any relevant logs or proof-of-concept

> [!WARNING]
> When sharing logs, configuration, or state, **redact secrets**. Never paste a
> HubSpot access token (`pat-…`) or the contents of a `terraform.tfstate` file
> (it may contain sensitive attribute values) into a report or issue.

## Response

We aim to acknowledge a report within **3 business days**, agree on a
disclosure timeline, and credit reporters who wish to be named once a fix is
released. Fixes ship in a new release; per our release policy, published
versions are immutable and are never rewritten in place.

## Supported Versions

This provider is pre-1.0. Security fixes are applied to the latest released
minor version. Once 1.0 is released, this section will list the supported
version range.

## Scope

This provider authenticates with a HubSpot service key or private-app token and
manages **portal configuration only** — it never requests CRM record scopes.
Reports concerning credential handling, token exposure in state or logs, or the
provider's HTTP client are in scope. Vulnerabilities in HubSpot's own APIs
should be reported to [HubSpot](https://www.hubspot.com/security).
