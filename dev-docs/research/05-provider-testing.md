# Testing Terraform Providers: Unit & Integration Methods (researched July 2026)

Decision guide for which method tests which layer. Framework: `terraform-plugin-testing` v1.16.0.

## Layer map

| Layer | Method | Needs TF binary? | Needs creds? |
|---|---|---|---|
| Pure logic (flatten/expand, ID parsers, normalizers, custom types) | Table-driven Go tests + `go-cmp` | No | No |
| Schemas, validators, plan modifiers | Direct framework-component invocation | No | No |
| Client wire behavior (serialization, pagination, retry, error decoding) | Real client → `httptest` fake server (or go-vcr cassettes) | No | Only to record |
| Full CRUD lifecycle, hermetic | `resource.UnitTest` against `httptest` fake | Yes | No |
| Full CRUD lifecycle, real API | `resource.Test`/`ParallelTest` + sweepers | Yes (`TF_ACC=1`) | Yes |

## 1. Unit testing (no Terraform binary)

### Pure logic
Officially recommended unit targets: model↔API conversion (flatten/expand), composite-ID parse/build round-trips, normalization/**semantic-equality** functions. Table-driven, `t.Parallel()`, `cmp.Diff`. Framework `types` values (`types.StringValue`, null/unknown variants) are plain Go values — constructible directly. Semantic-equality functions are the highest-ROI target: bugs there surface as unreproducible "provider produced inconsistent result" errors. Docs: https://developer.hashicorp.com/terraform/plugin/testing/unit-testing

### Framework components directly
Everything is an interface taking request/response structs:

- **Schema validation test** (one per resource — cheap, catches wiring bugs early): call `Schema(ctx, SchemaRequest{}, &resp)` then `resp.Schema.ValidateImplementation(ctx)`, assert no diagnostics.
- **Validators**: build `validator.StringRequest{ConfigValue: ...}`, call `ValidateString`, assert diagnostics. Always include null and unknown cases (validators must no-op). Canonical style: terraform-plugin-framework-validators' own tests.
- **Plan modifiers**: construct `planmodifier.StringRequest{StateValue, PlanValue, ConfigValue}`, assert `resp.PlanValue`/`resp.RequiresReplace`. Test state-null (create), plan-null (destroy), equal-values paths.

### HTTP mocking — three approaches
1. **`httptest` + configurable base URL — the provider-land idiom.** Provider schema exposes `endpoint`/base-URL; tests point the *real* client at `httptest.NewServer`. Reference implementation: `hashicorp/terraform-provider-http` (every test spins a server, incl. TLS/mTLS variants).
2. **`jarcoal/httpmock`** — transport patching by URL regex; useful for generated clients that can't be re-pointed; less common.
3. **go-vcr cassettes (record/replay)** — the Datadog/Google model for large API surfaces:
   - Datadog: env-var mode selection (`RECORD=true|false|none` → record/replay/passthrough); one cassette per test; **deny-by-default header allowlist** + URL secret scrubbing in an AfterCaptureHook; custom matcher on method+URL+normalized JSON body; **frozen clock** (`clockwork.FakeClock` persisted per test) so timestamps/resource names replay deterministically; explicit `depends_on` in multi-resource configs for reproducible request order; `isTestOrg()` guard so record mode can't run against production.
   - Google (magic-modules): PR CI replays cassettes credential-free; a bot re-records failed tests against live GCP internally — fork PRs get real-API coverage without secrets.
   - Tradeoff: httptest fakes encode *your assumptions* and can drift from reality; VCR records reality but cassettes rot and need sanitization discipline. Rule of thumb: **httptest for small/medium hand-written clients (us); VCR once endpoint count makes hand-fakes costly.**

**Interface-mocking the client (gomock/mockery) is not the idiom** — most CRUD bugs live below an interface mock (serialization, pagination, retry), and building `resource.CreateRequest` by hand from raw tftypes is brittle. Narrow interface mocks are OK for scripting exact error sequences (429-retry orchestration); default to real-client-vs-fake-server.

## 2. Acceptance testing (`terraform-plugin-testing`)

### Anatomy
`TestAcc*` funcs gated on `TF_ACC`; each `TestStep` runs real plan/apply/refresh/destroy with the in-process provider over gRPC. Key TestCase fields: `ProtoV6ProviderFactories`, `PreCheck`, `CheckDestroy`, `TerraformVersionChecks` (`tfversion.SkipBelow`), `ErrorCheck` (convert environmental errors to skips), `ExternalProviders`. Step modes: Config lifecycle, `RefreshState`, `ImportState`, `PlanOnly`, `ExpectError`, `Taint`, `PreConfig` (out-of-band mutation hook), `PostApplyFunc` (v1.14+).

### Modern assertions — use these, not legacy `TestCheckResourceAttr`
- **plancheck**: `ExpectEmptyPlan`, `ExpectNonEmptyPlan`, `ExpectResourceAction(addr, ResourceActionUpdate|Replace|...)`, `ExpectUnknownValue`, `ExpectSensitiveValue` — in `ConfigPlanChecks{PreApply|PostApplyPreRefresh|PostApplyPostRefresh}` / `RefreshPlanChecks`.
- **statecheck**: `ExpectKnownValue(addr, tfjsonpath.New("attr"), knownvalue.StringExact("x"))`, `CompareValue` across steps (replaces ID-tracking recreation hacks), identity checks (v1.13+).
- **knownvalue**: exact/regex/collection/partial-object checks; **tfjsonpath** for navigation.
- Canonical **no-perpetual-diff test**: re-apply same config with `PreApply: ExpectEmptyPlan()`.
- Config sources: inline `fmt.Sprintf` (majority pattern), or `ConfigDirectory: config.TestNameDirectory()` + `ConfigVariables` for real `.tf` files.

### Import tests (every importable resource)
`ImportState: true` + `ImportStateVerify` (+ `ImportStateVerifyIgnore` for attrs the API never returns, `ImportStateIdFunc` for composite IDs). `ImportStateKind` (v1.13+): test both `ImportCommandWithID` and `ImportBlockWithID`; `ImportBlockWithResourceIdentity` if resource identity is implemented (TF ≥1.12).

### Drift / disappears / upgrade tests
- **`_disappears` test** (AWS convention): create → `PreConfig` deletes out-of-band via API client → `RefreshState: true, ExpectNonEmptyPlan: true` — asserts Read maps 404 to `RemoveResource` + recreation, not an error.
- Refresh-only steps with `RefreshPlanChecks.PostRefresh: ExpectEmptyPlan()` for no-drift assertions.
- **Upgrade-state test**: step 1 `ExternalProviders` pins the released version from the registry; step 2 switches to local build and asserts `ExpectEmptyPlan()` — the standard no-breaking-change gate.

### Hermetic acceptance tests — the sweet spot
Run the entire lifecycle against an `httptest` fake via `resource.UnitTest` (skips the TF_ACC gate but uses a real Terraform binary): real plan/apply/state, zero credentials, seconds per test. The fake can script failure modes real APIs can't reliably produce: 429s (verifies retry), 500-then-200, eventual consistency (404 on first GET after create), partial responses. This is the PR-runnable CRUD coverage for a SaaS provider.

### Sweepers
`TestMain` → `resource.TestMain(m)`; `resource.AddTestSweepers(name, &resource.Sweeper{Dependencies, F})`; delete resources matching the `tf-acc-test` prefix (+ created-before-N-hours filter). Run via `make sweep` nightly.

### `terraform test` (.tftest.hcl)
A **module/config** testing tool, not a provider harness — its `mock_provider` replaces provider code entirely. Legit provider-repo uses: smoke-testing `examples/` and e2e of a released build via `dev_overrides`. Don't use as the CRUD harness.

### OpenTofu
Same suite runs against OpenTofu: `TF_ACC_TERRAFORM_PATH` → `tofu` binary, `TF_ACC_PROVIDER_HOST=registry.opentofu.org` (+ `TF_ACC_PROVIDER_NAMESPACE`); CI via `opentofu/setup-opentofu` with `tofu_wrapper: false`. Wrinkle: OpenTofu plan JSON may include ephemeral-resource entries counted as non-empty plans — conditionally adjust `ExpectNonEmptyPlan` for tofu runs.

### Test-account strategy (SaaS)
Dedicated test portal only; **verify at runtime the credentials belong to the test account** before destructive modes (Datadog's `isTestOrg()` — adapt: check portal ID from `/account-info/v3/details`). `acctest.RandomWithPrefix("tf-acc-test")` names everywhere (the sweeper contract). `resource.ParallelTest` by default; serialize rate-limited/singleton resources (webhook settings, default pipeline). PR tier = unit + hermetic; nightly tier = real API full suite + sweep.

## 3. CI patterns

- **PR (no secrets, always runs)**: lint (`golangci-lint`) → `make generate` + `git diff --exit-code` (tfplugindocs drift gate) → unit + hermetic tests across matrix `terraform: ['1.13.*','1.14.*']` × OpenTofu latest (`hashicorp/setup-terraform` / `opentofu/setup-opentofu`, wrappers off). Pin actions by SHA.
- **Cred-gated real-API**: GitHub does not expose secrets to fork PRs. Options: run on push-to-main + nightly `schedule` + `workflow_dispatch`; label-gated `ok-to-test` flow (review before checkout of untrusted code); Datadog model (PRs replay-only, maintainers re-record). Nightly = full suite + sweepers, results to issue/Slack.
- **Flake management**: nightly job separate from PR gate; `gotestsum --rerun-fails` / skip-with-tracking-issue quarantine; `ErrorCheck` converts environmental errors to skips.

## 4. Complementary tooling

- `golangci-lint` with HashiCorp's provider set (errcheck, forcetypeassert, staticcheck, unparam, usetesting, …).
- `tfproviderlint`: SDKv2-era; adopt selectively (AT-series test-hygiene checks still pay off for framework providers).
- `tfplugindocs validate` + generate-diff as CI tests.
- **Native Go fuzzing** on §1 pure functions: ID parsers (`parse(build(x))==x`, no panics), normalizers (idempotence) — `-fuzztime 30s` in CI.
- Property-based (`rapid`) on flatten/expand round-trips is defensible but not established practice in mainstream providers; optional layer.

## Recommended stack for this provider

1. **Every resource**: schema `ValidateImplementation` test; table-driven flatten/expand + ID-parser tests; validator/plan-modifier unit tests where custom.
2. **Every resource**: one hermetic `resource.UnitTest` CRUD+import lifecycle vs an `httptest` fake HubSpot (configurable `base_url` — already in the provider config design), with plancheck/statecheck assertions. Fake scripts 429/eventual-consistency/name-purgatory scenarios.
3. **Every resource**: real-portal `TestAcc*` suite — basic, update, `_disappears`, import (command + block kinds), second-apply `ExpectEmptyPlan`, upgrade-state pin once released. `ParallelTest` + `tf-acc-test` prefix + sweepers; portal-ID guard before destructive runs.
4. **CI**: PR = lint + unit + hermetic × {TF n-1, n} × OpenTofu; nightly = real API + sweep (secrets only in scheduled/label-gated workflows); tfplugindocs gates.
5. Revisit go-vcr (Datadog-style harness) if/when the endpoint surface outgrows hand-written fakes.

Key sources: [plugin testing docs](https://developer.hashicorp.com/terraform/plugin/testing) · [import mode](https://developer.hashicorp.com/terraform/plugin/testing/acceptance-tests/import-mode) · [sweepers](https://developer.hashicorp.com/terraform/plugin/testing/acceptance-tests/sweepers) · [AWS contributor guide](https://hashicorp.github.io/terraform-provider-aws/running-and-writing-acceptance-tests/) · [Google magic-modules tests](https://googlecloudplatform.github.io/magic-modules/test/test/) · [Datadog provider harness](https://github.com/DataDog/terraform-provider-datadog) · [terraform-provider-http (hermetic exemplar)](https://github.com/hashicorp/terraform-provider-http) · [Grafana provider (docker/cloud split)](https://github.com/grafana/terraform-provider-grafana) · [go-vcr integration issue](https://github.com/hashicorp/terraform-plugin-testing/issues/190)
