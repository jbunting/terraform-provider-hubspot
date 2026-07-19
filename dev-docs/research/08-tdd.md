# Test-Driven Development for This Provider (researched July 2026)

How to adapt red-green-refactor to provider development, where the meaningful test is a full Terraform lifecycle. Complements `05-provider-testing.md` (methods catalog); this doc is the *workflow*.

## The core tension, and how it resolves

The ecosystem prefers acceptance tests over unit tests ("works exactly as expected in real world use cases" — HashiCorp), and plan-time behavior (plan modifiers, unknowns, RequiresReplace) only exists inside the Terraform protocol exchange — so the test TDD says to write first *is* the acceptance test. But each TestStep drives a real `terraform` binary through init→plan→apply→refresh→plan→destroy: minutes and money against a real API.

**The resolution is hermetic acceptance tests**: an in-process stateful `httptest` fake with the endpoint injected via env/provider attribute keeps the full TF lifecycle (real plan/apply semantics) at ~1–3 s per run — a genuine red-green loop. This is why `base_url` configurability is a day-one design decision. `terraform-provider-http` (HashiCorp's own) runs its entire suite this way.

Big providers don't practice strict test-first (AWS = "tests-with", enforced at review; Google = spec-first codegen where tests are generated with the resource), but official docs contain a clear red-green endorsement: **regression tests must be committed separately, demonstrating the failure, before the fix commit** — and AWS's honesty criterion: tests "must fail if the code were to be removed."

## The recipe for every new resource

1. **Design the HCL first.** Write `examples/resources/hubspot_<name>/resource.tf` — the config users should write. Review it as a design doc. This file later feeds tfplugindocs, so it's not throwaway.
2. **Write the lifecycle acceptance test as the executable spec (red).** TestSteps: create + `statecheck.ExpectKnownValue` checks, update (superset of basic; pin update-vs-replace with `plancheck.ExpectResourceAction`), `ImportState` + `ImportStateVerify`, and a `_disappears` step. Include a CheckExists helper that queries the (fake) API. Stub the resource registration so the suite is red, not broken.
3. **Stand up the hermetic fake.** Stateful in-memory store + mux implementing just the endpoints this resource needs (~100 lines per resource family). `t.Setenv("HUBSPOT_API_URL", srv.URL)`. Script HubSpot quirks into it as they're specified: 429s, archive semantics, name purgatory, server normalization (displayOrder rewrites).
4. **Unit-TDD the seams in parallel.** Table-driven tests *first* for flatten/expand and normalizers (milliseconds). The cheapest genuine TDD in provider work: write the round-trip property `expand(flatten(m)) == m` (with `rapid` or plain tables) and idempotence properties for normalizers (`n(n(x)) == n(x)`) **before** implementing — catches the "Read drops a field ⇒ perma-diff" bug class.
5. **Implement CRUD until green.** Schema → Create → Read → Update → Delete → Import. The harness's automatic post-apply refresh+plan is a free idempotency oracle — any Create/Read state mismatch goes red immediately.
6. **Refactor with the suite as a net**, then enable the real-portal variant of the same test (endpoint switched by env) for CI/nightly — never the inner loop.

## Fast-feedback tooling

- `TF_ACC=1 go test ./internal/provider -run 'TestAccPipeline_basic$' -count=1 -v` — one test, no cache surprises. `gotestsum --watch` for save-triggered reruns.
- `TF_ACC_TERRAFORM_PATH=$(which terraform)` — stop the harness downloading a binary per environment.
- **Debugging acceptance tests**: provider code runs in the test process, so `dlv test ./internal/provider -- -test.run TestAccX` hits breakpoints in CRUD directly. For debugging against a real terraform CLI: `dlv exec --headless ./terraform-provider-hubspot -- -debug` + export the printed `TF_REATTACH_PROVIDERS`.
- `TF_LOG=DEBUG` for protocol-level diagnosis of plan-time weirdness.
- `resource.ParallelTest` + randomized names once tests are hermetic.

## Keeping the fake honest (contract testing)

HubSpot publishes OpenAPI 3.0 specs ([HubSpot-public-api-spec-collection](https://github.com/HubSpot/HubSpot-public-api-spec-collection); live catalog at `GET https://api.hubspot.com/api-catalog-public/v1/apis`). Use them two ways:

1. **Spec-validate the fake**: wrap fake handlers in [kin-openapi](https://github.com/getkin/kin-openapi) request/response validation middleware — any fake response violating the real schema fails the test. Optionally generate server stubs with `oapi-codegen` so fake types can't drift.
2. **Nightly contract runs**: the *same* acceptance tests run against a real HubSpot sandbox portal on schedule (the Google/Datadog "replay on PR, live on schedule" pattern). Divergence fails the nightly and prompts a fake update.

Stateless spec-mockers (Prism, openapi-mock) can't serve lifecycle tests (Create-then-Read must return what was created) — the handwritten stateful fake + spec validation is the right combination. go-vcr cassettes are the alternative once endpoint count grows, but note: you can't record a cassette for code that doesn't exist yet — cassettes serve regression loops, not green-field test-first.

## Anti-patterns (documented, avoid)

- **Attribute-echo tests**: asserting a configured value round-trips into state mostly tests the framework — passes even when Read is a no-op returning config. The real contract: remote API state matches TF state (CheckExists against the API/fake) + the automatic empty-replan. Reserve value checks for Computed attributes and defaults.
- **Unit-only coverage missing plan-time behavior**: perma-diffs and "inconsistent result after apply" only manifest through real plan cycles — encode `ExpectEmptyPlan` / `ExpectResourceAction` into the spec. (Sharp edge: `ExpectEmptyPlan` ignores output changes.)
- **Tests written to match the implementation**: counter-discipline = failing-test-first commit for every bug fix.
- **Missing `_disappears` coverage**: out-of-band deletion is the most common real-world failure untested by happy paths — mandatory test class (AWS).
- **Skipping the update step**: every non-immutable attribute needs an update TestStep (Google's rule) — create-only tests miss the largest bug class.
- **Cassette rot / cassette-shaped tests**: replay suites drift without scheduled re-recording, and replay determinism constraints (forced `depends_on`) can under-test ordering behavior.

## Policy for this repo

1. New resource = HCL example first, then the red lifecycle test against the hermetic fake, then implementation. The failing test is the design review artifact.
2. Every bug fix lands as two commits: failing regression test, then fix.
3. Flatten/expand and normalizers get property/table tests before implementation.
4. `_disappears` and update steps are mandatory per resource, enforced at review.
5. Fake handlers carry kin-openapi validation against HubSpot's published specs; nightly runs the suite against the sandbox portal.

Key sources: [testing patterns](https://developer.hashicorp.com/terraform/plugin/testing/testing-patterns) · [framework acceptance tests](https://developer.hashicorp.com/terraform/plugin/framework/acctests) · [plan checks](https://developer.hashicorp.com/terraform/plugin/testing/acceptance-tests/plan-checks) · [debugging](https://developer.hashicorp.com/terraform/plugin/debugging) · [AWS acceptance-test guide](https://hashicorp.github.io/terraform-provider-aws/running-and-writing-acceptance-tests/) · [magic-modules tests](https://googlecloudplatform.github.io/magic-modules/test/test/) · [Datadog TESTING.md](https://github.com/DataDog/terraform-provider-datadog/blob/master/TESTING.md) · [terraform-provider-http hermetic suite](https://github.com/hashicorp/terraform-provider-http) · [HubSpot OpenAPI specs](https://github.com/HubSpot/HubSpot-public-api-spec-collection)
