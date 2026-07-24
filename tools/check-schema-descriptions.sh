#!/usr/bin/env bash
# Description-coverage CI gate (dev-docs/research/06-documentation.md §5):
# every provider, resource, and data-source attribute must carry a non-empty
# description, because MarkdownDescription is the user-facing API surface in
# the registry, `terraform providers schema -json`, and editor hovers.
#
# Builds the provider into a temporary filesystem mirror, extracts the live
# schema with `terraform providers schema -json` (or `tofu`; override the
# binary with TF_BIN), and asserts coverage with jq.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
tf_bin="${TF_BIN:-terraform}"

command -v "$tf_bin" >/dev/null || { echo "error: $tf_bin not on PATH (set TF_BIN to a terraform/tofu binary)" >&2; exit 1; }
command -v jq >/dev/null || { echo "error: jq not on PATH" >&2; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# Build the provider into an unpacked filesystem-mirror layout so a pinned
# registry release is never consulted — the check always runs against HEAD.
platform="$(go env GOOS)_$(go env GOARCH)"
mirror="$work/mirror/registry.terraform.io/revosai/hubspot/99.0.0/$platform"
mkdir -p "$mirror"
(cd "$repo_root" && go build -o "$mirror/terraform-provider-hubspot_v99.0.0" .)

cat >"$work/cli.tfrc" <<EOF
provider_installation {
  filesystem_mirror {
    path    = "$work/mirror"
    include = ["registry.terraform.io/revosai/hubspot"]
  }
  direct {
    exclude = ["registry.terraform.io/revosai/hubspot"]
  }
}
EOF

mkdir -p "$work/config"
cat >"$work/config/main.tf" <<'EOF'
terraform {
  required_providers {
    hubspot = {
      source = "revosai/hubspot"
    }
  }
}
EOF

export TF_CLI_CONFIG_FILE="$work/cli.tfrc"
(cd "$work/config" && "$tf_bin" init -backend=false -input=false >/dev/null)
(cd "$work/config" && "$tf_bin" providers schema -json) >"$work/schema.json"

# Walk every attribute (including nested_type attributes and nested blocks)
# and every resource/data-source block description; print the path of each
# entry whose description is empty.
missing="$(jq -r '
  def walk_block($ctx):
    ((.attributes // {}) | to_entries[] as $a |
      (if (($a.value.description // "") == "") then "\($ctx): attribute \($a.key)" else empty end),
      (($a.value.nested_type // empty) | walk_block("\($ctx).\($a.key)"))
    ),
    ((.block_types // {}) | to_entries[] as $b |
      ($b.value.block | walk_block("\($ctx).\($b.key)")));

  .provider_schemas[] as $p |
  (
    ($p.provider.block | walk_block("provider")),
    (($p.resource_schemas // {}) | to_entries[] as $r |
      (if (($r.value.block.description // "") == "") then "\($r.key): resource description" else empty end),
      ($r.value.block | walk_block($r.key))),
    (($p.data_source_schemas // {}) | to_entries[] as $d |
      (if (($d.value.block.description // "") == "") then "data.\($d.key): data-source description" else empty end),
      ($d.value.block | walk_block("data.\($d.key)")))
  )
' "$work/schema.json")"

if [ -n "$missing" ]; then
  echo "Missing schema descriptions (every attribute needs a MarkdownDescription):" >&2
  echo "$missing" >&2
  exit 1
fi

echo "OK: every provider/resource/data-source attribute has a description."
