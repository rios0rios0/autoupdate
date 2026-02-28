# Contributing

Contributions are welcome. By participating, you agree to maintain a respectful and constructive environment.

For coding standards, testing patterns, architecture guidelines, commit conventions, and all
development practices, refer to the **[Development Guide](https://github.com/rios0rios0/guide/wiki)**.

## Prerequisites

- [Go](https://go.dev/dl/) 1.26+
- [Make](https://www.gnu.org/software/make/) 4.0+
- [Git](https://git-scm.com/downloads) 2.30+

## Development Workflow

1. Fork and clone the repository
2. Create a branch: `git checkout -b feat/my-change`
3. Install dependencies:
   ```bash
   go mod download
   ```
4. Build the project:
   ```bash
   make build
   ```
5. Run the application locally:
   ```bash
   make run
   ```
6. Build a debug binary (with debug symbols):
   ```bash
   make debug
   ```
7. Run tests:
   ```bash
   go test ./...
   ```
8. Install the binary to `~/.local/bin`:
   ```bash
   make install
   ```
9. Update `CHANGELOG.md` under `[Unreleased]`
10. Commit following the [commit conventions](https://github.com/rios0rios0/guide/wiki/Life-Cycle/Git-Flow)
11. Open a pull request against `main`

## Adding a New Provider

Implement the `domain.Provider` interface and register it in `cmd/run.go`:

```go
reg.Register("bitbucket", bitbucket.New)
```

## Adding a New Updater

Implement the `domain.Updater` interface and register it in `cmd/run.go`:

```go
reg.Register(npmUpdater.New())
```
