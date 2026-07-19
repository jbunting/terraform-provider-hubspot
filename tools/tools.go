//go:build generate

// Package tools tracks build-time tool dependencies and hosts go:generate
// directives for documentation generation.
//
// NOTE: before the first `make generate`, run
//
//	go get github.com/hashicorp/terraform-plugin-docs
//
// so the tfplugindocs dependency is present in go.mod.
package tools

import (
	// Documentation generation
	_ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
)

// Format Terraform code for use in documentation.
//go:generate terraform fmt -recursive ../examples/

// Generate documentation.
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-dir .. -provider-name hubspot
