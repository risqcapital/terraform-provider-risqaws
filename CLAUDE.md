# CLAUDE.md — AI Assistant Guide for terraform-provider-risqaws

This file provides context for AI coding assistants (such as Claude) working in this repository.

---

## Project Overview

This is a custom **Terraform Provider** for AWS, published to the Terraform Registry at `registry.terraform.io/risqcapital/risqaws`. It extends the standard HashiCorp AWS provider with specialized resources needed by RISQ Capital.

- **Language:** Go 1.24.0
- **Framework:** HashiCorp Terraform Plugin Framework v1 (plugin framework, not SDK v2)
- **AWS SDK:** aws-sdk-go-v2 (not v1)
- **License:** MPL-2.0

---

## Repository Structure

```
terraform-provider-risqaws/
├── main.go                          # Provider entry point
├── go.mod / go.sum                  # Go module dependencies
├── GNUmakefile                      # Build and development targets
├── .golangci.yml                    # Linting configuration
├── .goreleaser.yml                  # Multi-platform release builds
├── terraform-registry-manifest.json # Terraform Registry metadata
├── CHANGELOG.md                     # Version history
├── internal/
│   └── provider/
│       ├── provider.go              # Provider schema and AWS config setup
│       ├── provider_test.go         # Provider test factory
│       ├── ssm_command_resource.go  # risqaws_ssm_command resource
│       ├── ssm_command_resource_test.go
│       ├── glue_catalog_resource.go # risqaws_glue_catalog resource
│       └── glue_catalog_resource_test.go
├── tools/
│   ├── tools.go                     # go:generate directives for docs/fmt
│   ├── go.mod
│   └── go.sum
├── examples/
│   └── resources/
│       ├── risqaws_ssm_command/resource.tf
│       └── risqaws_glue_catalog/
│           ├── s3tables_glue_catalog.tf
│           └── versions.tf
└── docs/
    ├── index.md                     # Auto-generated provider docs
    └── resources/ssm_command.md     # Auto-generated resource docs
```

---

## Development Workflows

### Common Commands

```bash
# Format Go code
make fmt

# Run linter
make lint

# Build and install the provider locally
make install

# Run code generation (docs + terraform example formatting)
make generate

# Run unit tests with coverage
make test

# Run acceptance tests (requires real AWS credentials)
make testacc

# Full default workflow: fmt + lint + install + generate
make
```

### Running Tests

**Unit tests** (no AWS required):
```bash
go test ./internal/provider/
```

**Acceptance tests** (requires AWS credentials + `TF_ACC=1`):
```bash
TF_ACC=1 go test -v -cover ./internal/provider/ -timeout 120m
```

Acceptance tests are prefixed with `TestAcc`. They are skipped automatically if `TF_ACC` is not set or if the `CI` environment variable is set in the `testAccPreCheck` helper.

### Code Generation

Documentation is auto-generated using `tfplugindocs`. After adding or modifying resources, regenerate docs:

```bash
make generate
```

This runs `tfplugindocs generate` (via `tools/tools.go`) and also runs `terraform fmt -recursive` on the `examples/` directory. Always commit the generated `docs/` changes together with your code changes.

---

## Adding a New Resource

Follow this pattern for every new resource:

1. **Create the file:** `internal/provider/<service>_<resource>_resource.go`
2. **Implement the required interfaces:**
   - `resource.Resource` (mandatory)
   - `resource.ResourceWithConfigure` (for AWS client access)
   - `resource.ResourceWithImportState` (if import is supported)
3. **Define the model struct** with `tfsdk` field tags:
   ```go
   type MyResourceModel struct {
       Id   types.String `tfsdk:"id"`
       Name types.String `tfsdk:"name"`
   }
   ```
4. **Register the resource** in `internal/provider/provider.go` inside `Resources()`:
   ```go
   func (p *RisqAwsProvider) Resources(ctx context.Context) []func() resource.Resource {
       return []func() resource.Resource{
           NewSsmCommandResource,
           NewGlueCatalogResource,
           NewMyNewResource, // add here
       }
   }
5. **Add examples** under `examples/resources/risqaws_<resource>/resource.tf`
6. **Run `make generate`** to regenerate docs
7. **Add acceptance tests** in `<service>_<resource>_resource_test.go`

---

## Code Conventions

### Naming

| Context | Convention | Example |
|---|---|---|
| Terraform resource type | `risqaws_<snake_case>` | `risqaws_ssm_command` |
| Go resource constructor | `New<PascalCase>Resource` | `NewSsmCommandResource` |
| Go resource struct | `<PascalCase>Resource` | `SsmCommandResource` |
| Go model struct | `<PascalCase>ResourceModel` | `SsmCommandResourceModel` |
| Nested model structs | `<PascalCase>TypeModel` | `SsmTargetTypeModel` |

### Framework Patterns

- Use `diag.Diagnostics` for all error and warning reporting. Never `panic` or `log.Fatal` in resource code.
- Fatal errors → `resp.Diagnostics.AddError("Summary", detail)`
- Retriable/non-fatal issues → `resp.Diagnostics.AddWarning("Summary", detail)`
- Always pass and propagate `context.Context` through all function calls.
- Use `plan.Get(ctx, &data)` / `state.Set(ctx, &data)` for state management.
- Use Terraform Plugin Framework types (`types.String`, `types.Bool`, etc.) in model structs, not raw Go primitives.

### AWS SDK v2 Usage

- Load AWS config once at the provider level in `provider.go` via `config.LoadDefaultConfig(ctx)`.
- Pass config via `resp.ResourceData` / `resp.DataSourceData` using the `Configure` method.
- Instantiate service-specific clients in the resource's `Configure` method (e.g., `ssm.NewFromConfig(cfg)`).
- For region overrides, instantiate a new client with `aws.Config{Region: region}` after copying the base config.

### Polling Long-Running Operations

For async AWS operations (e.g., SSM Run Command), use an exponential backoff polling loop. See `ssm_command_resource.go` for the reference implementation using:
- `PollCommandInvocation()` pattern
- Backoff starting at 1s, doubling up to a max of 30s
- Configurable timeout via `timeouts` block

### Plan Modifiers

Use plan modifiers in the schema for immutable fields:
```go
PlanModifiers: []planmodifier.String{
    stringplanmodifier.RequiresReplace(),
},
```

### Resource Lifecycle Stubs

If a resource is effectively immutable (create-only), it is acceptable to leave `Read`, `Update`, and `Delete` as no-ops. Document this in a comment.

---

## Linting

Linting is enforced by `golangci-lint` using `.golangci.yml`. Enabled linters include:

`copyloopvar`, `durationcheck`, `errcheck`, `forcetypeassert`, `godot`, `ineffassign`, `makezero`, `misspell`, `nilerr`, `predeclared`, `staticcheck`, `unconvert`, `unparam`, `unused`, `usetesting`

The configuration uses `max-issues-per-linter: 0` (zero tolerance). Run `make lint` before committing.

Generated files, `examples/`, `third_party/`, and `builtin/` directories are excluded from linting.

---

## CI/CD

### Pull Request Checks (`.github/workflows/test.yml`)

Three jobs run on every PR (ignoring README-only changes):

| Job | What it does |
|---|---|
| `build` | `go build -v .` + `golangci-lint run` |
| `generate` | `make generate` + git diff (fails if generated files are out of date) |
| `test` | Acceptance tests across Terraform versions 1.0–1.4 (`TF_ACC=1`) |

Always ensure `make generate` is run and committed before opening a PR.

### Releases (`.github/workflows/release.yml`)

Triggered by pushing a `v*` tag. Uses GoReleaser to:
- Build binaries for Linux, Windows, Darwin, FreeBSD (amd64, 386, arm, arm64)
- Disable CGO for static binaries
- Sign artifacts with GPG
- Publish to GitHub Releases

---

## Implemented Resources

### `risqaws_ssm_command`

Runs an SSM document on targeted EC2 instances.

- **File:** `internal/provider/ssm_command_resource.go`
- **AWS APIs:** `ssm:SendCommand`, `ssm:GetCommandInvocation`
- **Key behaviors:**
  - Create-only resource (no meaningful Read/Update/Delete)
  - Polls for command completion with exponential backoff
  - Supports `timeouts` block for create timeout
  - Targets instances via key/values blocks

### `risqaws_glue_catalog`

Manages an AWS Glue Catalog resource.

- **File:** `internal/provider/glue_catalog_resource.go`
- **AWS APIs:** `glue:CreateCatalog`, `glue:GetCatalog`, `glue:UpdateCatalog`, `glue:DeleteCatalog`
- **Key behaviors:**
  - Full CRUD lifecycle
  - Supports federated catalogs (S3 Tables, Redshift)
  - Supports Lake Formation default permissions for databases and tables
  - Region override via optional `region` attribute
  - `name` attribute requires replacement on change

---

## Dependency Management

```bash
# Add a new dependency
go get github.com/some/package

# Tidy and remove unused dependencies
go mod tidy

# Update all dependencies
go get -u ./...
go mod tidy
```

Dependencies are managed by Go modules (`go.mod`, `go.sum`). Always commit both files after changes.

The `tools/` directory has its own `go.mod` for code generation dependencies and is separate from the provider's module.

---

## Key Files Reference

| File | Purpose |
|---|---|
| `main.go` | Provider binary entry point; sets registry address and version |
| `internal/provider/provider.go` | Provider schema, AWS config loading, resource registration |
| `GNUmakefile` | All development targets |
| `.golangci.yml` | Linting rules |
| `.goreleaser.yml` | Release build configuration |
| `tools/tools.go` | `go:generate` directives for docs and formatting |
| `terraform-registry-manifest.json` | Terraform Registry protocol version metadata |
