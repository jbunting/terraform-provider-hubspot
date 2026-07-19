# Releasing the Provider: Best Practices (researched July 2026)

Release engineering conventions, verified against scaffolding-framework, AWS, Datadog, Grafana, Cloudflare, PagerDuty workflows, and both registries' docs.

## 1. Artifacts & pipeline

Every GitHub release must contain exactly:

| Artifact | Naming |
|---|---|
| Per-platform zips | `terraform-provider-hubspot_{VERSION}_{OS}_{ARCH}.zip`; binary inside named `terraform-provider-hubspot_v{VERSION}` |
| Registry manifest | `..._manifest.json` (from repo-root `terraform-registry-manifest.json`: `{"version":1,"metadata":{"protocol_versions":["6.0"]}}`) |
| Checksums | `..._SHA256SUMS` (covers zips **and** manifest) |
| Signature | `..._SHA256SUMS.sig` — **binary detached GPG sig, never ASCII-armored** |

Missing manifest ⇒ registry assumes protocol 5.0 — silently wrong for framework providers.

**GoReleaser (v2 config)** — copy from scaffolding-framework: `CGO_ENABLED=0`, `-trimpath`, `mod_timestamp: '{{ .CommitTimestamp }}'` (reproducible builds), `ldflags -X main.version={{.Version}}`, goos linux/darwin/windows/freebsd × amd64/386/arm/arm64 (minus darwin-386, windows-arm — the ones that matter: linux_amd64/arm64, darwin_amd64/arm64, windows_amd64), `signs.args` with `--batch --detach-sign` (no `--armor`), `changelog.disable: true` (CHANGELOG managed separately).

**Workflow**: tag-push `v*` trigger, `permissions: contents: write` only, `fetch-depth: 0`, `crazy-max/ghaction-import-gpg` (secrets `GPG_PRIVATE_KEY` + `PASSPHRASE`), `goreleaser release --clean`. **Pin all actions by commit SHA.** Alternative: call HashiCorp's reusable workflow `hashicorp/ghaction-terraform-provider-release` (`community.yml`). Hardening beyond static secrets: OIDC→Vault-fetched GPG material (Grafana pattern); protect the `v*` tag namespace with rulesets.

**GPG**: RSA or DSA only — **ECC/ed25519 keys are rejected** by the Terraform Registry. Register the armored *public* key in registry settings. Rotation = upload new key, sign future releases with it, **never delete old keys** (would break verification of published versions). OpenTofu registry **checks key expiry at submission** — use non-expiring keys.

## 2. Registry publishing

- **Terraform Registry**: one-time GitHub-OAuth publish + GPG key upload → webhook auto-ingests every release within minutes. Stalled ingestion: delete stale webhooks + Resync in the UI.
- **OpenTofu Registry**: submit provider **and** GPG key via **issue-form templates** on `opentofu/registry` (PRs rejected); submitter's org membership must be public. After listing, versions are picked up by periodic scan (slower than TF Registry, ~within the hour).
- **Failure modes**: trailing comma in manifest JSON; armored .sig; a git *branch* named like the version tag; draft GitHub releases (ignored — don't use `release.draft: true`); ECC key.
- **A published version is immutable.** Re-tagging changes checksums and breaks every consumer's `.terraform.lock.hcl`. Fix forward with a new patch release, always.

## 3. Changelog & version bumping

Three proven patterns:
1. **`.changelog/{PR#}.txt` + hashicorp/go-changelog** (AWS/Google house style): typed fences (`release-note:breaking-change|new-resource|new-data-source|enhancement|bug|note`), CI enforces entry presence, CHANGELOG generated at release.
2. **changie** — same fragment model, better ergonomics (Terraform core uses it).
3. **Conventional commits + git-cliff/release-please** (Grafana): PR-title lint enforces convention; CI validates the tag's semver against commit history (blocks under-bumped tags).

CHANGELOG format (HashiCorp spec): `## X.Y.Z (Unreleased)` on main; sections BREAKING CHANGES → NOTES → FEATURES → ENHANCEMENTS → BUG FIXES; entries `* resource/hubspot_x: message [GH-123]`. Operator-impacting changes only.

**Bumping**: derivable from entry types (any breaking-change → major; new-resource/enhancement → minor; only bug → patch) but keep a human in the loop for majors — provider semver has state-compatibility semantics commit types don't capture. Best combo: human tags, machine verifies (Grafana).

**Tag creation models**: manual maintainer tag (default, fine for us) · release-PR-merge that creates the tag itself (Datadog — auditable) · fully automated (release-please). Fixed cadence (AWS: weekly Thursdays) beats "when ready" once there's traffic.

## 4. Prereleases & backports

- Prerelease tags (`v2.0.0-beta.1`) are safe: **Terraform/OpenTofu range operators (`~>`) never select prereleases** — users must pin exactly. Use for major-version betas and to rehearse the full pipeline before v0.1.0.
- Backports: dominant model is **main-only, fix-forward**; security backports case-by-case. If an old major must be supported: `release/vN-1` branch from the last tag, cherry-pick, tag — the tag-triggered workflow works from any branch (Datadog keeps a `v3` branch this way). Don't promise old-major support for a small provider.

## 5. Quality gates

**Pre-tag (PR CI):**
- `goreleaser check` + `goreleaser release --snapshot --clean --skip=sign,publish` — proves all platform builds compile *before* tagging (a failed release workflow after tagging burns the version number).
- **Upgrade-state test**: step 1 `ExternalProviders` pins the published previous release from the registry, step 2 local build + `ExpectEmptyPlan()`. Mandatory before majors and any SchemaVersion bump; state upgraders ship **in the same release** as the schema change.
- Last scheduled full acceptance run green before tagging.

**Post-release:**
- Clean-dir `terraform init` (and `tofu init`) with the new version pinned — verifies ingestion, signature, lock-file hashes end-to-end.
- `gpg --verify` + `shasum -c` on downloaded artifacts.
- `dev_overrides` is for pre-release local testing only — it bypasses the registry path, so it can't verify a release.

## 6. Supply chain

- Registries verify **only GPG**; attestations are supplementary. Real-world example: `integrations/terraform-provider-github` ships GitHub **artifact attestations** (`actions/attest-build-provenance` over dist/*.zip + SHA256SUMS, `permissions: id-token: write, attestations: write`); SLSA L3 possible via slsa-github-generator. Nice-to-have, not required.
- Dependabot/Renovate on gomod + github-actions. **Go stdlib CVEs require rebuilding and shipping a patch release** (binaries statically embed the runtime) — a routine reason for otherwise-empty patches.
- CVE response: GitHub Security Advisory → fix on main → immediate patch release → publish GHSA. Compromised signing key: upload new key, contact terraform-registry@hashicorp.com (old versions stay valid under the old key).

## 7. Versioning policy (what counts as breaking)

- **Major**: removing/renaming resources or attributes; attribute type or value-format changes; ID/import-format changes; **default changes incompatible with existing state**; auth/config precedence changes; raising minimum Terraform version. Sharpest edge: anything making a previously-applied config produce a non-empty plan or fail refresh is breaking, even if schema looks additive.
- **Minor**: new resources/attributes/validations, and **deprecations** (deprecate in a minor with `DeprecationMessage` + NOTES entry + upgrade-guide mention; remove in the next major; ≥1 minor of warning).
- **Patch**: bug fixes only.
- Majors ≤ ~once/year, each with a `docs/guides/version-N-upgrade.md` registry-rendered guide written *before* and published *with* the release.
- Protocol: framework = v6 = Terraform ≥1.0, declared in the manifest. Document the supported Terraform/OpenTofu range in the README and mirror it in the acceptance matrix.

## Key links

- Publishing: https://developer.hashicorp.com/terraform/registry/providers/publishing
- Registry FAQ (keys, immutability): https://developer.hashicorp.com/terraform/registry/faq
- Versioning spec: https://developer.hashicorp.com/terraform/plugin/best-practices/versioning
- Reference configs: https://github.com/hashicorp/terraform-provider-scaffolding-framework
- Reusable workflow: https://github.com/hashicorp/ghaction-terraform-provider-release
- OpenTofu registry procedures: https://github.com/opentofu/registry/blob/main/PROCEDURES.md · https://opentofu.org/docs/cli/plugins/signing/
- go-changelog: https://github.com/hashicorp/go-changelog · changie: https://changie.dev
- Upgrade-test pattern: https://developer.hashicorp.com/terraform/plugin/framework/migrating/testing
