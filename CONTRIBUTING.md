# Contributing

Contributions are welcome. By participating, you agree to maintain a respectful and constructive environment.

For coding standards, testing patterns, architecture guidelines, commit conventions, and all
development practices, refer to the **[Development Guide](https://github.com/rios0rios0/guide/wiki)**.

## Prerequisites

- [Go](https://go.dev/dl/) 1.26+
- [Make](https://www.gnu.org/software/make/)
- [chlog](https://github.com/luizjhonata/chlog) (`go install github.com/luizjhonata/chlog@latest`)

## Development Workflow

1. Fork and clone the repository
2. Create a branch: `git checkout -b feat/my-change`
3. Install dependencies:
   ```bash
   go mod download
   ```
4. Build the binary:
   ```bash
   make build
   ```
5. Make your changes
6. Validate:
   ```bash
   make lint
   make test
   make sast
   ```
7. Add a changelog fragment — never edit `CHANGELOG.md`, which is generated from them:
   ```bash
   chlog new --kind Added --body 'added the thing that was not there before'
   ```
8. Commit following the [commit conventions](https://github.com/rios0rios0/guide/wiki/Git-Flow)
9. Open a pull request against `main`

## Adding a New Provider

Git providers are implemented in [gitforge](https://github.com/rios0rios0/gitforge). Once the
provider exists there, register it in `internal/infrastructure/repositories/container.go` — a
token-less adapter for URL matching and a factory for token-bound instances:

```go
reg.RegisterAdapter(bitbucket.NewProvider(""))
reg.RegisterFactory("bitbucket", bitbucket.NewProvider)
```

## Adding a New Updater

1. Create a package under `internal/infrastructure/repositories/<ecosystem>/`.
2. Implement `repositories.UpdaterRepository` (`Name()`, `Detect()`, `CreateUpdatePRs()`). Implement
   `repositories.LocalUpdater` (`ApplyUpdates()`) as well so the updater joins the clone-once batch
   pipeline instead of the legacy per-updater clone.
3. Register it in `internal/infrastructure/repositories/container.go`:

```go
reg.Register(npmRepo.NewUpdaterRepository())
```

4. Add the ecosystem's default entry (`enabled`, `auto_complete`) to `configs/autoupdate.yaml`.
   That file is the built-in defaults layer, embedded in the binary and also fetched from `main` at
   runtime, beneath the user's own config.
5. If the ecosystem should also work in local mode (`autoupdate .`), add it to the maps keyed by
   langforge `Language` in `internal/domain/commands/local_command.go`: `updaterNameForLanguage`,
   `localUpgradeHandlers` and `prContentGenerators`.
6. Record dependency changes through `internal/support/changelog.go` — never write `CHANGELOG.md`
   from an updater directly, or the Keep a Changelog and chlog paths will drift apart.
