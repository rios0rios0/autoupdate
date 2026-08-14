# Contributing

Contributions are welcome. By participating, you agree to maintain a respectful and constructive environment.

For coding standards, testing patterns, architecture guidelines, commit conventions, and all
development practices, refer to the **[Development Guide](https://github.com/rios0rios0/guide/wiki)**.

## Prerequisites

- [Go](https://go.dev/dl/) 1.26+
- [Make](https://www.gnu.org/software/make/)

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
7. Update `CHANGELOG.md` under `[Unreleased]`
8. Commit following the [commit conventions](https://github.com/rios0rios0/guide/wiki/Life-Cycle/Git-Flow)
9. Open a pull request against `main`

## Adding a New Provider

Implement the `domain.Provider` interface and register it in `cmd/run.go`:

```go
reg.Register("bitbucket", bitbucket.New)
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

4. Add the ecosystem to `configs/autoupdate.yaml`. That file is fetched from `main` at runtime and
   merged over the user's config, so the entry is what enables the updater for existing installs.
5. If the ecosystem should also work in local mode (`autoupdate .`), add it to the two maps in
   `internal/domain/commands/local_command.go`. They are keyed by langforge `Language` and a test
   asserts they cover every constant, so a new langforge language fails the build until both are
   updated.
6. Record dependency changes through `internal/support/changelog.go` — never write `CHANGELOG.md`
   from an updater directly, or the Keep a Changelog and chlog paths will drift apart.
