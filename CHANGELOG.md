# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

When a new release is proposed:

1. Create a new branch `chore/bump-x.x.x` (this isn't a long-lived branch!!!) — the name prefix matters: the release pipeline only tags merges whose branch is named `chore/bump-`;
2. The Unreleased section on `CHANGELOG.md` gets a version number and date (AutoBump does this automatically);
3. Open a Pull Request with the bump version changes targeting the `main` branch;
4. When the Pull Request is merged, the release pipeline detects the `chore/bump-x.x.x` branch in the merge commit and automatically creates the Git tag and GitHub release.

Releases to productive environments should run from a tagged version.
Exceptions are acceptable depending on the circumstances (critical bug fixes that can be cherry-picked, etc.).

## [Unreleased]

## [0.21.3] - 2026-08-22

### Changed

- changed the Go version to `1.27.0` and updated all module dependencies
- changed the per-ecosystem version pin rewrites to share one emitter, `support.VersionPinUpdateScript`,
  covering `.java-version`, `.python-version` and `.ruby-version`
- changed the five per-ecosystem `Dockerfile` base-image rewrites to share one emitter,
  `support.DockerfileTagUpdateScript`, so the walk and the version guard it depends on have a single
  spelling -- five hand-copied versions of that loop is how the guard came to be missing from some
  of them in the first place
- changed the Go module dependencies to their latest versions

### Fixed

- fixed the bash half of the version guard ordering pre-release identifiers as plain strings while the
  Go half followed Semantic Versioning precedence. `rc.10` sorts below `rc.9` as text but above it as a
  version, so for a pin like a .NET SDK preview the two halves disagreed: Go named the branch, the
  commit and the pull request after an upgrade the script then declined to write. Both halves now
  compare identifiers left to right, numerically where they are numeric, ranking a numeric identifier
  below an alphanumeric one and a longer set above its own prefix
- fixed the version pin rewrites downgrading a repository that tracks a release ahead of the one the
  release feed reports. Every pin was compared with a plain "is it different?" check, so a `.nvmrc`
  reading `26.7.0` was rewritten to the `24.19.0` LTS the Node.js feed returns -- inside a pull request
  titled as an upgrade. A rewrite now requires the fetched release to be strictly newer, and the rule
  lives in one place (`support.IsNewerVersion` and the bash guard it emits for the generated upgrade
  scripts) rather than being restated per ecosystem. It covers `.nvmrc`, `.node-version`,
  `.python-version`, `.ruby-version`, `.java-version`, `global.json`, `.fvmrc`, the `go` directive, the
  base image tags in a `Dockerfile` and the language versions in a CI pipeline
- fixed the Go directive being written back to the target version after `go mod tidy` raised it, which
  turned a dependency's Go requirement into a downgrade on the next run
- fixed the base image tags in a `Dockerfile` being rewritten whenever the language pin moved, even when
  the image was already newer than the version being rolled out
- fixed pins that name no version at all -- `lts/*` in a `.nvmrc`, `system` in a `.ruby-version`, a JRuby
  or TruffleRuby release -- being replaced with a version number from an unrelated release channel
- fixed `isDockerHubImage` splitting an image name with `strings.SplitN` where `strings.Cut` says the
  same thing, which `golangci-lint` 2.13 reports as a `modernize` finding
- fixed build metadata being read as a pre-release when two pins are compared. Semantic Versioning
  excludes everything after a `+` from precedence, so `1.0.0+build.1` and `1.0.0` name the same release;
  reading it as a pre-release instead rewrote one to the other as though a pre-release were being
  promoted, and refused the genuine upgrade from `1.0.0-rc.1` to `1.0.0+build.1`
- fixed GitHub Actions workflows never being upgraded on the clone-based path. `support.WalkFilesByExtension` and `support.WalkFilesByPredicate` refused to descend into any directory whose name began with a dot, which put `.github/workflows/` out of reach of the pipeline updater's local scan. Because the pipeline updater implements `LocalUpdater`, `autoupdate run` always takes that clone-based path, so a repository was matched by the detector, cloned, scanned to no effect and reported as up to date — only a root `azure-pipelines.yml` / `.azure-pipelines.yml` and `azure-devops/` were ever reachable on disk. Both action pins (`uses: owner/repo@v4`) and language versions (`go-version:`, `python-version:`) inside workflows were affected
- fixed the walkers conflating "hidden" with "not the repository's own source". The blanket dot-directory rule is replaced by an explicit deny list of trees a repository does not author — version-control metadata (`.git`, `.hg`, `.svn`), vendored code (`vendor`, `node_modules`) and tool caches (`.terraform`, `.terragrunt-cache`, `.venv`, `.gradle`, `.dart_tool`, `.next` and friends) — shared by both walkers through a single traversal so the rule cannot drift between them. A deny list was chosen over an allow list of hidden directories that matter because the bug being fixed *is* a missing entry in an implicit allow list: `.gitea/workflows/`, `.forgejo/workflows/` and `.woodpecker/` would each have been invisible in turn. As a consequence the Dockerfile updater now also sees committed hidden sources such as `.devcontainer/Dockerfile`, and no longer rewrites base images in `node_modules/` or `vendor/` copies that could never reach the pull request while still being counted in the changelog and PR body. Go module discovery is unchanged: `moduleDirsFromPaths` keeps dropping hidden and vendored segments so it stays in step with the `find` in the generated upgrade script

## [0.21.2] - 2026-08-17

### Changed

- changed the Go module dependencies to their latest versions

## [0.21.1] - 2026-08-16

### Changed

- changed the Go module dependencies to their latest versions

## [0.21.0] - 2026-08-15

### Added

- added `.fvmrc` SDK bumping, the Flutter counterpart of `.ruby-version` and `.nvmrc`. Wherever Go can see the worktree — local mode and the batch path — the file is rewritten by parsing and re-emitting the JSON, so keys the tool does not know about survive. The remote path clones inside the generated script and never exposes the worktree to Go, so there it is a `sed` that substitutes only the value of the `"flutter"` key rather than reconstructing the document; a `flavors` block is left intact either way. Only a project the manifest identifies as Flutter has its pin touched: `.fvmrc` names a Flutter SDK, so a plain Dart package carrying one would otherwise be pinned to a version from the Dart release channel. `environment: sdk:` in `pubspec.yaml` is deliberately left alone: raising a package's SDK floor is a compatibility decision for its consumers, and `pub upgrade --major-versions` already raises it when a dependency actually requires it
- added a `dart` updater covering Flutter as well, detected by `pubspec.yaml`. It runs `pub upgrade --major-versions` rather than a plain `pub upgrade`: the plain form only re-resolves `pubspec.lock` inside the constraints already declared, so it would never actually raise a dependency, while `--major-versions` rewrites the constraints in `pubspec.yaml` to what pub reports as resolvable. Pub applies that rewrite through `yaml_edit`, so the rationale comments a `pubspec.yaml` carries between its dependency entries survive it
- added SDK version fetchers reading Google's own release channels. **Neither Dart nor Flutter has an endoflife.date product** — `api/dart.json` and `api/flutter.json` are both 404 — so the versions come from the Dart archive's stable `VERSION` document and the Flutter releases index. The Flutter index lists every release ever published and is *not* ordered by recency, so the current stable is resolved through the hash `current_release` points at rather than by taking the first match
- added toolchain selection through langforge's `dart.IsFlutter`, the single place that distinguishes the two: a Flutter project is driven with `flutter pub` and a plain package with `dart pub`, because `dart pub get` cannot resolve the SDK-sourced packages (`flutter`, `flutter_test`, `flutter_localizations`) that every Flutter project depends on. When a repository pins its SDK with FVM, pub is routed through `fvm` — otherwise the upgrade would run against whatever toolchain happened to be on `PATH` rather than the one the project is built with

### Changed

- changed every updater's generated script to build its git credential setup from one shared helper, `support.GitAuthScript`. The same `insteadOf` rewrites had been copied into ten places and had already drifted: only some of them rewrote Azure DevOps SSH remotes, so a dependency declared over SSH failed to authenticate depending on which ecosystem the repository happened to be. They now all do
- changed the `langforge` dependency from a commit pseudo-version to the released `v1.0.0`. The `dart` updater above needed `pkg/infrastructure/languages/dart`, which no published version carried, so the pin had to name a commit until langforge cut a release; it now names a tag like every other dependency. `v1.0.0` removes the per-ecosystem `Provider` structs and the two Java runtime managers, neither of which this repository ever named — it uses the per-language `Detector` types, `DetectWith`, `dart.IsFlutter` and `dart.IsFlutterManifest`, all unchanged
- changed the Go version to `1.26.6` and updated all module dependencies
- changed the local-mode updaters to open the repository, stash, and branch through one `gitlocal.PrepareBranch` call, and to run their generated script through `cmdrunner.RunScript`. The script is now written outside the repository in every mode, so it can no longer appear as an untracked file in the worktree the caller inspects next

### Fixed

- fixed `CONTRIBUTING.md`, which told contributors to register updaters in a `cmd/run.go` that no longer exists
- fixed the `README.md` ecosystem table, which documented two updaters while nine shipped, and the accompanying "All 6 updaters" comment in the sample configuration

## [0.20.7] - 2026-08-13

### Changed

- changed the Go module dependencies to their latest versions

## [0.20.6] - 2026-08-12

### Changed

- changed the Go module dependencies to their latest versions

## [0.20.5] - 2026-08-11

### Changed

- changed the Go module dependencies to their latest versions

## [0.20.4] - 2026-08-07

### Changed

- changed the Go module dependencies to their latest versions

### Fixed

- fixed the Python package-manager selection so a repository that installs from a `requirements.txt`
  and has never committed a `pdm.lock` keeps pip, even when its `pyproject.toml` declares
  `[tool.pdm]` — that table is also how a pip project states its package layout. Those repositories
  were upgraded through PDM, which resolved a lock file from scratch (the whole of the resulting
  pull request, since `pdm update` leaves the pyproject's own constraints alone) and never touched
  the `requirements.txt` the build installs from. A committed `pdm.lock` still selects PDM

## [0.20.3] - 2026-08-04

### Changed

- refreshed `.github/copilot-instructions.md` to document the shared chlog/changelog writer and the
  Python package-manager selection invariant, bringing it in line with `CLAUDE.md`

## [0.20.2] - 2026-07-31

### Changed

- changed the Python PDM detection to stop re-fetching a `pyproject.toml` the repository does not
  have. A provider's `HasFile` is itself a file fetch, so every `requirements.txt`-only repository —
  the common pip layout — spent a second request per run to rediscover the same absence, for nothing
  but the provider's rate limit

### Fixed

- fixed the Python updater being able to migrate a repository to a different package manager while
  bumping its dependencies. A repository whose only manifest is a `requirements.txt` is managed by
  pip, and PDM is now selected only when a `pyproject.toml` is actually present — that file is PDM's
  project definition, so running `pdm update` without one makes PDM write a fresh `pyproject.toml`,
  turning a pip project into a PDM project inside what was meant to be a dependency bump. A
  `pdm.lock` with no `pyproject.toml` beside it no longer counts as a PDM project. The pip upgrade
  additionally discards a `pyproject.toml` or `pdm.lock` that appeared while it ran, so no manifest
  the repository did not already own can reach the pull request
- fixed local mode reporting PDM in the pull request description and dry-run log for a repository it
  had upgraded with pip. The dependency manager was resolved once for the commands to run and a
  second time, under a weaker rule, for the report; both now read the same value, resolved once from
  the manifests the repository carried before the upgrade started

## [0.20.1] - 2026-07-30

### Changed

- changed the Go module dependencies to their latest versions

## [0.20.0] - 2026-07-29

### Added

- added support for [chlog](https://github.com/luizjhonata/chlog), the fragment-based changelog
  tool. A repository that commits a `.chlog.yaml` (or simply carries a `.changes/unreleased/`
  directory) now gets one fragment file per update instead of an edit to the `[Unreleased]` section
  of its `CHANGELOG.md`. chlog exists precisely to keep that file out of the merge path, so the
  previous behaviour put automated pull requests back into conflict with every hand-written entry.
  Detection is automatic, honours the `changesDir`, `unreleasedDir` and `kinds` declared by
  `.chlog.yaml`, and covers every ecosystem in both local and batch mode. A repository that does not
  use chlog is unaffected

### Changed

- changed the changelog handling of all nine ecosystems to run through one shared implementation
  instead of a near-identical copy per ecosystem, so the two formats cannot drift apart. The bash
  fragment the language updaters generate now takes its destination from the environment, which is
  what lets the same script write either a `CHANGELOG.md` or a chlog fragment

## [0.19.0] - 2026-07-28

### Added

- added automatic PDM detection to the Python updater: a repository carrying a `pdm.lock`, a
  `[tool.pdm]` table or the `pdm.backend` build backend is now upgraded with
  `pdm update --update-all --no-sync` instead of raw pip. PDM resolves against its own lock file,
  which pip cannot see, so a PDM project previously had its lock left untouched no matter how often
  the updater ran. `--no-sync` keeps the run to a resolution, because the refreshed `pdm.lock` is the
  only artefact worth committing

### Changed

- changed the Python pull request description and changelog entry to name the dependency manager the
  run actually used. The description previously advertised `pip install --upgrade -r
  requirements.txt` and asked reviewers to review `requirements.txt` even for projects that have no
  such file

### Fixed

- fixed the Python updater committing the `*.egg-info/` directory as though it were a dependency
  change. Installing a `pyproject.toml` project locally makes setuptools generate that directory;
  because it was untracked but not ignored, `git add -A` swept it into the commit, so a repository
  with no dependency movement at all still produced a pull request. The pattern is now added to
  `.gitignore`, and only when such a directory actually exists, so repositories that never build the
  project keep their `.gitignore` untouched
- fixed the pipeline updater leaving a stale version behind in `displayName` labels. Upgrading a
  task from `3.11` to `3.14` rewrote `versionSpec` but left `displayName: 'Use Python 3.11'`
  untouched, so the file described a version it no longer used. The label is now upgraded alongside
  the version field rather than having its version stripped out, and it is upgraded whether it is
  written above or below that field — a label written below sat outside the scan match and was never
  reached at all. The rewrite stops at the end of the enclosing step, so a later step mentioning the
  same version keeps its own label — including a non-task step such as `- script:`, which owns a
  label of its own — and a label reading `3.110` is left alone when the upgrade is from `3.11`

## [0.18.0] - 2026-07-27

### Added

- added an automatic cleanup of the dated branches left behind by earlier runs: before creating the
  branch for the current run, every remote branch carrying the aggregate prefix is deleted and the
  pull request attached to each one is closed (on Azure DevOps, abandoned). Because the aggregate
  branch is dated, an unattended daily run previously stacked up one abandoned branch per day for as
  long as nobody merged. A branch with no pull request at all is still deleted, because having
  nothing to close is a no-op rather than a failure; only a branch whose pull request could not be
  closed is left in place, so the pair stays retryable instead of stranding an open pull request
  whose source branch is gone. Each close call is bounded by a timeout, so an unresponsive provider
  degrades cleanup to best-effort rather than stalling the update run behind housekeeping
- added the `cleanup_stale_branches` configuration key and the `--skip-cleanup` flag to turn that
  cleanup off. Cleanup is opt-out, so it runs unless explicitly disabled; the flag overrides the
  configuration for a single run
- added the `aggregate_branch_prefix` configuration key to customise the `chore/autoupdate-` branch
  prefix. The same value names the branch a run creates and selects the branches cleanup removes, so
  the two can never point at different branches

### Changed

- changed cleanup to run only after the same-day pull request check has passed, so a pull request is
- changed the Go module dependencies to their latest versions
  never closed without a replacement being opened for it

### Fixed

- fixed the Gitleaks stage failing every build on `main`. The allowlisted fingerprints in
  `.gitleaksignore` embed the hash of the commit a finding came from, so when a rebase moved the two
  commits holding the historical git-remote URL false positives, all 14 entries stopped matching and
  the long-suppressed findings came back. Re-pointed every entry at the commits' current hashes

## [0.17.0] - 2026-07-22

### Added

- added nested Go module support to the Go updater: it now discovers every `go.mod` in a repository instead of only the one at the root, and applies the `go` directive bump, `go get -u -t ./...` and `go mod tidy` inside each module directory — `go get ./...` never crosses a module boundary, so a repository that keeps a Go module in a subdirectory (for example an integration-test harness living inside an infrastructure repository) had that module left permanently outdated; vendored, `testdata`, `node_modules` and hidden directories are skipped so their pinned manifests are not rewritten; whether a run counts as a Go version upgrade is now decided across every module rather than from the root alone, so a stale nested module no longer produces a PR whose title and changelog entry contradict each other

### Fixed

- fixed Go ecosystem detection skipping any repository whose only `go.mod` lives in a subdirectory: detection previously asked the provider for a root `go.mod` alone, so such repositories were never processed by the Go updater at all; the version context that drives the branch name and changelog wording now falls back to the first nested module when the root declares no module
- fixed the Go updater failing on a module directory whose name starts with `-`, which bash parsed as a `pushd` stack-index option
- fixed the Go updater treating the repository root as a nested module on Azure DevOps, whose API returns absolute item paths (`/go.mod`) rather than repo-relative ones
- fixed the Go updater writing non-existent Docker base image tags into `Dockerfile` `FROM` clauses: it now verifies each target `golang:<version>` tag is published on Docker Hub before rewriting, falls back to the closest published patch within the same minor and suffix, and leaves the clause untouched when no suitable image exists — instead of blindly applying the latest `go.dev` version via `sed`, which could point `FROM` at an unpublished patch (registry lag) or a dropped Alpine variant such as `golang:1.25.7-alpine3.20`
- fixed the Go upgrade script aborting midway when a `go.mod` declares no `go` directive: `grep` exits non-zero on no match, which under `set -o pipefail` killed the run and left the modules already processed half-upgraded — the reads now tolerate a missing directive, which also makes the existing "no directive" branch reachable
- fixed the Go version being dropped from the generated commit subject: the backticks around `$GO_VERSION` were unescaped, so bash expanded them as command substitution and committed `upgraded Go version to  and updated all dependencies`

## [0.16.9] - 2026-07-16

### Changed

- changed the Go module dependencies to their latest versions

### Security

- hardened directory permissions from `0o750`/`0o755` to owner-only `0o700` in `WriteFileChanges` and its test fixtures, resolving the Semgrep `incorrect-default-permission` CI failures (a directory needs the owner execute bit, so the rule's `0o600` file threshold is documented as inapplicable and suppressed per line)

## [0.16.8] - 2026-07-14

### Changed

- changed the Go module dependencies to their latest versions

## [0.16.7] - 2026-07-13

### Changed

- changed the Go module dependencies to their latest versions

## [0.16.6] - 2026-07-10

### Changed

- changed the Go module dependencies to their latest versions
- changed the Go version to `1.26.5` and updated all module dependencies

## [0.16.5] - 2026-07-03

### Changed

- changed the Go module dependencies to their latest versions

## [0.16.4] - 2026-07-02

### Changed

- changed the Go module dependencies to their latest versions

### Security

- replaced `secrets: inherit` with an explicit `CLAUDE_CODE_OAUTH_TOKEN` pass-through in the Claude Code workflows to satisfy the `secrets-inherit` least-privilege check

## [0.16.3] - 2026-06-24

### Changed

- changed the Go module dependencies to their latest versions

### Fixed

- fixed the CI/CD pipeline updater misreporting an Azure DevOps `GoTool` version as a Node.js upgrade: the `NodeTool` rule matched the wrong input field (`version` instead of `versionSpec`), so its multi-line pattern bled past the Node.js task and captured a later Go task's `version` (e.g. proposing a Node.js bump for a Go version such as `1.21`). Node.js versions are now read from the `NodeTool` `versionSpec` input, and each Azure DevOps task is scanned in isolation so a language rule can no longer capture a neighbouring task's version field

## [0.16.2] - 2026-06-18

### Changed

- changed the Go module dependencies to their latest versions
- refreshed `CLAUDE.md` and `.github/copilot-instructions.md` to document batch-mode concurrency (the `concurrency` config field / `--concurrency` flag and the mutex-guarded shared state in `RunCommand`)

## [0.16.1] - 2026-06-09

### Changed

- changed the Go module dependencies to their latest versions

### Fixed

- fixed the `gitleaks` security stage failing on `main` (which blocked releases) by suppressing 14 historical false-positive `Password in URL` findings via `.gitleaksignore` — these are git-remote auth URLs built from variables (`${AUTH_TOKEN}`, string concatenation) and fake test fixtures in pre-refactor commits, not real secrets

## [0.16.0] - 2026-06-08

### Added

- added parallel repository processing to `autoupdate run`, configurable via the `concurrency` config field and the `--concurrency` flag, so large organizations finish well within CI time limits

### Changed

- changed `autoupdate run` to process repositories within an organization in parallel (default 4 at a time) instead of one at a time; set `concurrency: 1` to restore the previous sequential behavior

## [0.15.8] - 2026-06-03

### Changed

- changed the Go version to `1.26.4` and updated all module dependencies

## [0.15.7] - 2026-05-25

### Changed

- changed the Go module dependencies to their latest versions

## [0.15.6] - 2026-05-22

### Changed

- changed the Go module dependencies to their latest versions

## [0.15.5] - 2026-05-20

### Changed

- changed the Go module dependencies to their latest versions

## [0.15.4] - 2026-05-19

### Changed

- changed the Go module dependencies to their latest versions
- refreshed `CLAUDE.md` to document the `gitlocal` infrastructure package, repo config loader in support utilities, and `exclude_repos`/per-repo opt-out config features

## [0.15.3] - 2026-05-08

### Changed

- changed the Go version to `1.26.3` and updated all module dependencies

## [0.15.2] - 2026-05-03

### Changed

- changed the Go module dependencies to their latest versions

## [0.15.1] - 2026-05-01

### Changed

- changed the Go module dependencies to their latest versions

## [0.15.0] - 2026-04-30

### Added

- added `exclude_repos` to the global `autoupdate.yaml`: a right-anchored glob list (`path.Match` semantics) matched against `<org>/<repo>` for GitHub/GitLab and `<org>/<project>/<repo>` for Azure DevOps. Honored in batch mode and in `autoupdate .` when a config file is loadable.
- added per-repository `.autoupdate.yaml` opt-out file (`skip: true` short-circuits both `autoupdate run` and `autoupdate .` before any updater work). The optional `reason` field is logged when the skip fires.

### Changed

- changed `LocalCommand` to detect the Git remote before language detection so excluded repos no longer require a supported project layout.
- changed the Go module dependencies to their latest versions

## [0.14.7] - 2026-04-29

### Changed

- changed the Go module dependencies to their latest versions

## [0.14.6] - 2026-04-28

### Changed

- refreshed `CLAUDE.md` and `.github/copilot-instructions.md` to document new ecosystems (Ruby, Java, C#, Dockerfile, Pipeline), `self-update`/`version` commands, `cliforge` dependency, and additional domain ports

## [0.14.5] - 2026-04-24

### Changed

- changed the Go module dependencies to their latest versions

## [0.14.4] - 2026-04-19

### Changed

- changed the Go module dependencies to their latest versions

## [0.14.3] - 2026-04-17

### Changed

- changed the Go module dependencies to their latest versions

## [0.14.2] - 2026-04-16

### Changed

- changed the Go module dependencies to their latest versions

## [0.14.1] - 2026-04-15

### Changed

- changed `BatchGitContext` to expose `HeadHash`, `RestoreSnapshot`, `AdvanceSnapshot`, and `FlattenToWorktree` so the aggregate pipeline can roll back individual updater failures without discarding earlier successes
- changed batch mode (`run` command) to produce one consolidated pull request per repository that bundles changes from every applicable updater into a single `chore/autoupdate-YYYY-MM-DD` branch, commit, and PR, instead of one PR per updater
- changed the Go version to `1.26.2` and updated all module dependencies

## [0.14.0] - 2026-04-14

### Added

- added Ruby, Java, and C# dependency updaters supporting version upgrades and dependency refresh
- added unit tests for Ruby, Java, and C# updater version fetchers and repository logic

### Changed

- changed the Go module dependencies to their latest versions

## [0.13.0] - 2026-04-03

### Added

- added `self-update` command to download and install the latest release from GitHub
- added `version` command to display the current build version

### Changed

- changed cliforge import paths to follow the new `pkg/` package restructuring
- changed the Go module dependencies to their latest versions

## [0.12.1] - 2026-03-31

### Changed

- changed the Go module dependencies to their latest versions

## [0.12.0] - 2026-03-30

### Added

- added GitHub Actions version detection and upgrading to the pipeline updater, supporting major version pins (`@v4` -> `@v5`) and full semver pins (`@v4.1.2` -> `@v4.2.0`)
- added unit test coverage from 17% to 55% across updater repositories, command runner, and support packages

### Changed

- changed the Go module dependencies to their latest versions

### Fixed

- fixed `replaceLastOccurrence` replacing version in trailing inline comments instead of the action reference when GitHub Actions lines contain comments repeating the version (e.g., `# pinned to v4`)
- fixed JavaScript updater creating misleading PRs when `npm update` only synced the project version in `package-lock.json` with zero actual dependency changes

## [0.11.1] - 2026-03-24

### Changed

- changed the Go module dependencies to their latest versions

### Fixed

- fixed the pipeline updater leaving stale version numbers in `displayName` fields when updating `versionSpec` in CI/CD pipeline files
- fixed the Python updater creating PRs without real dependency changes due to `pip freeze` capturing `file://` local path references from temp clone directories

## [0.11.0] - 2026-03-22

### Added

- added `DecodeSettings()` function for parsing YAML settings with optional strict mode
- added `MergeUpdatersConfig()` function for field-level deep merge of updater configurations
- added default config download and merge for the `updaters` section, following the autobump pattern of fetching defaults from GitHub and merging user overrides on top

### Changed

- changed `UpdaterConfig.AutoComplete` field from `bool` to `*bool` for proper field-level merge support
- changed `UpdaterConfig.Enabled` field to default to `true` when omitted from config, preventing updaters from being silently disabled when only `target_branch` or `auto_complete` is set
- changed the default `configs/autoupdate.yaml` to include all 6 registered updaters with sensible defaults
- changed the Go module dependencies to their latest versions

### Fixed

- fixed stale temporary directories and changelog files not being cleaned up after process termination
- fixed Terraform, Dockerfile, and Pipeline updaters generating changelog entries without backticks around code identifiers and version numbers, violating the CHANGELOG formatting standard
- fixed the pipeline updater replacing `displayName` instead of `versionSpec` in Azure DevOps pipeline files when both contained the same version string
- fixed the Python updater creating empty PRs when the upgrade script did not modify any files
- fixed the Terraform updater using non-production tags by validating that upgrade targets appear in the dependency repo's CHANGELOG.md

## [0.10.2] - 2026-03-19

### Changed

- changed the Go module dependencies to their latest versions

### Fixed

- fixed batch mode silently losing upgrade changes because `CreateBranchFromDefault` force-checkout wiped uncommitted `go.mod`/`go.sum` modifications
- fixed batch mode stash/pop safety by tracking whether a stash was created and verifying the stash ref before popping
- fixed potential auth token leak in upgrade script debug logs by redacting tokens from output

## [0.10.1] - 2026-03-18

### Changed

- changed local mode to auto-stash uncommitted changes instead of refusing to run on dirty worktrees, restoring to the original branch after the upgrade completes

### Fixed

- fixed Go dependency updater using deprecated `go get -u all` pattern that fails to detect updates in modern Go versions, replaced with `go get -u -t ./...`
- fixed local mode stash restore that could pop an unrelated stash entry or restore onto the wrong branch

## [0.10.0] - 2026-03-17

### Added

- added `exclude_forks` and `exclude_archived` settings to filter out fork and archived repositories during discovery

### Changed

- changed `gitforge` dependency from `v0.6.2` to `v0.7.0`, which includes sanitized clone URL logging, `IsFork`/`IsArchived` fields on `Repository`, and improved org-to-user discovery fallback

### Fixed

- fixed Go and JavaScript updaters running `CHANGELOG.md` updates and creating branches even when no dependency changes were detected
- fixed pipeline updater proceeding with file writes and `CHANGELOG.md` updates when version replacements produced no actual file changes

## [0.9.3] - 2026-03-17

### Fixed

- fixed pipeline updater failing to fetch latest Java version (HTTP 404) by upgrading `langforge` to `v0.4.0` which now uses the Amazon Corretto endpoint on `endoflife.date`

## [0.9.2] - 2026-03-17

### Changed

- changed `gitforge` dependency to `v0.6.2`, adding support for SSH config aliases in GitHub URL parsing
- changed `langforge` dependency to `v0.3.1`

## [0.9.1] - 2026-03-16

### Changed

- changed `gitforge` dependency to `v0.6.0`, picking up fixes for branch checkout with unstaged changes and GPG passphrase prompt in CI

## [0.9.0] - 2026-03-13

### Added

- added `gpg_key_path`, `gpg_key_passphrase`, `github_access_token`, `gitlab_access_token`, and `azure_devops_access_token` configuration fields
- added `LocalUpdater` interface for updaters that work on locally cloned repositories
- added GPG/SSH commit signing support in batch mode (`run` command) via `BatchGitContext`
- added multi-token authentication retry for batch mode git push operations
- added transport auto-detection (SSH vs HTTPS) for batch mode push

### Changed

- changed all six updaters (Terraform, Pipeline, Dockerfile, Go, Python, JavaScript) to implement the `LocalUpdater` interface for local filesystem operations in the batch pipeline
- changed batch mode (`run` command) to use a clone-based pipeline with centralized git operations (clone, branch, commit, push) instead of per-updater git management
- changed Go, Python, and JavaScript updater batch scripts to contain only language-specific operations (removed git clone, commit, and push from batch bash scripts)
- changed remote file checker to use `langforge`'s shared `fileutil.NewFileChecker()`, `IsGlobPattern()`, and `ExtractExtension()` utilities
- changed the Go module dependencies to their latest versions
- changed token resolution to use `gitforge`'s shared `ResolveTokenFromEnv()` and `TokenEnvHint()`, eliminating duplicated env var mapping logic
- changed version fetchers to use `langforge`'s shared `pkg/infrastructure/versions` package, eliminating duplicated HTTP fetch logic

### Removed

- removed `internal/infrastructure/repositories/versions/` package (moved to `langforge`)

## [0.8.0] - 2026-03-12

### Added

- added GPG and SSH commit signing support in local mode (reads from git config `commit.gpgsign` and `gpg.format`)
- added SSH push support in local mode (auto-detected from remote URL)

### Changed

- changed `serviceTypeToProviderName()` to use `gitforge`'s shared `ServiceTypeToProviderName()`, eliminating cross-CLI duplication
- changed commit signing resolution to use `gitforge`'s shared `ResolveSignerFromGitConfig()`, eliminating cross-CLI duplication
- changed local mode push to use `gitforge`'s adapter pattern instead of hardcoded provider-username map, supporting SSH and HTTPS with auth method retry
- changed push transport detection and auth retry to use `gitforge`'s shared `PushWithTransportDetection()`, eliminating cross-CLI duplication
- changed the Go version to `1.26.1` and updated all module dependencies

## [0.7.0] - 2026-03-09

### Added

- added `Dockerfile` updater for detecting and upgrading base image versions in `FROM` clauses via Docker Hub API
- added pipeline updater for detecting and upgrading hardcoded language versions in CI/CD configuration files (GitHub Actions and Azure DevOps YAML templates)
- added shared version fetcher package for Go, Python, Node.js, Java, and Terraform latest version resolution

### Changed

- upgraded `gitforge` dependency from `v0.1.1` to `v0.2.0`, bringing Azure DevOps PR creation fixes and GPG signing improvements
- upgraded `langforge` dependency to `v0.2.0` and removed local `replace` directive

### Fixed

- fixed the Go updater deps-only branch name to use `chore/upgrade-go-deps` instead of embedding the Go version number

## [0.6.0] - 2026-03-06

### Added

- added `github.com/rios0rios0/langforge` dependency for centralized language detection
- added `LocalGitContext` wrapper in `internal/infrastructure/repositories/gitlocal/` using go-git for branch creation, clean check, staging, committing, and pushing (replacing bash-generated git commands in local mode)
- added `RemoteFileChecker` and `DetectRemote` utilities in `internal/support/` bridging `langforge`'s `FileChecker` abstraction with `gitforge`'s's remote provider API
- added unit tests for `LocalGitContext` covering all public methods with BDD pattern

### Changed

- changed all `gitforge`'s import paths to the new DDD `pkg/` structure (e.g. `domain/entities` → `pkg/global/domain/entities`, `infrastructure/providers/github` → `pkg/providers/infrastructure/github`)
- changed Go, Python, and JavaScript local-mode updaters to use `LocalGitContext` (go-git) for git operations instead of generating bash scripts for branch creation, clean check, commit, and push
- changed local-mode bash scripts to contain only language-specific operations (auth setup, dependency upgrades, Dockerfile updates, changelog updates)
- changed the Go module dependencies to their latest versions
- replaced `projectType` enum and `switch`/`case` dispatch in `runLocalUpgrade` and `generatePRContent` with mapper pattern using `langforge`'s `Language` constants
- replaced custom `ProviderRegistry` with a thin wrapper around `gitforge`'s's `ProviderRegistry`, delegating factory registration and provider creation while adding `FileAccessProvider` type assertion
- replaced duplicated per-updater `Detect()` logic with `langforge`'s `DetectWith` + `RemoteFileChecker` abstraction across all 4 updaters (Go, Python, JavaScript, Terraform)
- replaced hardcoded `detectProjectType()` in local mode with `langforge`'s `LanguageRegistry.Detect()` for centralized language detection
- replaced inline `parseRemoteURL`, `parseAzureDevOpsURL`, and `parseStandardGitURL` with `gitforge`'s's `ParseRemoteURL` to consolidate duplicated code
- replaced local `ProviderConfig` struct, `ResolveToken()`, and `FindConfigFile()` with `gitforge`'s's shared implementations
- replaced raw struct literals in tests with `testkit`'s builders for consistent test data construction

### Fixed

- fixed `exhaustive` findings by adding missing `Language` and `ServiceType` keys to mapper functions in local command
- fixed `gochecknoglobals` finding by converting `InsertChangelogEntry` from function variable to regular function
- fixed `gochecknoglobals` findings by converting global map variables to function returns
- fixed `revive` `context-as-argument` finding by reordering `DetectRemote` parameters so `context.Context` is first

## [0.5.0] - 2026-02-14

### Added

- added `github.com/rios0rios0/testkit`'s dependency for test builders
- added build tags (`//go:build unit`) to all test double files
- added entity builders (`RepositoryBuilder`, `DependencyBuilder`) following `testkit`'s `BaseBuilder` pattern in `test/domain/entitybuilders/`
- added organized test doubles in `test/domain/commanddoubles/` and `test/infrastructure/repositorydoubles/`

### Changed

- changed the Terraform updater PR description to show a compact summary instead of a full table when there are more than 5 dependency upgrades
- created `RunController` and `LocalController` as cobra CLI adapters in `internal/infrastructure/controllers/`
- extracted `RunCommand` from `application/service.go` and `LocalCommand` from `cmd/local.go` into `internal/domain/commands/`
- introduced `AppInternal` to aggregate all controllers via DIG injection
- introduced `Controller` interface with `GetBind()` and `Execute()` following separation of concerns principles
- moved all code under `internal/` package for proper Go encapsulation
- moved entry point from `main.go` to `cmd/autoupdate/main.go` with separate `dig.go` for DI bootstrap
- refactored config loading from `config/` package into `internal/domain/entities/settings.go`
- refactored entire project to follow DDD/Clean Architecture patterns
- replaced manual registry-based dependency injection with `go.uber.org/dig` container
- restructured domain layer into `entities/`, `commands/`, and `repositories/` packages
- restructured infrastructure layer into `controllers/` and `repositories/` packages
- split `domain/models.go` into per-entity files under `internal/domain/entities/`

### Fixed

- fixed a potential nil pointer dereference in the Terraform HCL parser when `ParseHCL` returned a nil file without diagnostics errors

## [0.4.0] - 2026-02-12

### Added

- added container image reference scanning in `.hcl` (Terragrunt) files, detecting patterns like `relayer_http_image = "relayer-http:0.7.0"` and upgrading them to the latest Git tag from the same organization
- added JavaScript updater supporting npm, yarn, and pnpm projects (auto-detected via `lockfiles`), with automatic `.nvmrc`/`.node-version` and Dockerfile `node:` image tag updates
- added Python and JavaScript support to the standalone local mode (`autoupdate .`), with automatic project type detection
- added Python updater supporting `requirements.txt` and `pyproject.toml` projects, with automatic `.python-version` and Dockerfile `python:` image tag updates

### Changed

- changed the local mode to auto-detect Go, Python, and JavaScript projects instead of requiring `go.mod`
- changed the Terraform updater to scan both `.tf` and `.hcl` files, supporting mixed Terraform module and container image dependency upgrades in a single PR

## [0.3.0] - 2026-02-12

### Added

- added automatic `Dockerfile` image tag update when the Go version is upgraded, searching all `Dockerfiles` in the project tree (`Dockerfile`, `Dockerfile.*`, `*.Dockerfile`)

## [0.2.2] - 2026-02-10

### Fixed

- fixed a bug that created empty PRs where only `CHANGELOG.md` changed

## [0.2.1] - 2026-02-09

### Changed

- corrected Markdown formatting of Go-updater generated changelog entries

## [0.2.0] - 2026-02-09

### Added

- added `--token` flag for explicit auth token override in local mode
- added automatic CHANGELOG.md entry insertion when target repositories contain a changelog following the Keep a Changelog format (both Go and Terraform updaters)
- added dual branch naming patterns for the Go updater (`chore/upgrade-go-X.Y.Z` for version bumps, `chore/upgrade-deps-X.Y.Z` for dependency-only updates)
- added shared `InsertChangelogEntry` domain helper for Keep a Changelog manipulation
- added standalone local mode (`autoupdate .`) to update a local repository directly, auto-detecting the Git provider from the remote URL

### Changed

- changed the Go updater to re-apply the Go version after `go mod tidy` in case it normalizes three-part versions
- changed the Go updater to use portable `sed` with redirect-and-move instead of `sed -i` for cross-platform compatibility (GNU/BSD)
- changed the Go updater to verify `sed` modifications and handle missing `go` directives before setting version-update status flags
- changed the Terraform updater branch naming to use `chore/upgrade-` prefix format

## [0.1.3] - 2026-02-09

### Fixed

- fixed the bug with the GoLang PRs removing the minor version while updating

## [0.1.2] - 2026-02-07

### Changed

- changed the code to ensure Git user identity is configured for committing

## [0.1.1] - 2026-02-07

### Fixed

- fixed the bug with the Azure DevOps formatting wrong URLs

## [0.1.0] - 2026-02-07

### Added

- added Clean Architecture project structure (`domain/`, `application/`, `infrastructure/`, `cmd/`)
- added comprehensive test suite with hand-crafted test doubles (spies, stubs, dummies)
- added extensible updater plugin interface with Terraform and Go implementations
- added multi-provider support (GitHub, GitLab, Azure DevOps) for repository discovery and PR creation
- added project boilerplate (`CHANGELOG.md`, `Makefile`, `LICENSE`, `.editorconfig`, `.gitignore`, `.github/`)
- added YAML-based configuration with environment variable expansion for tokens

### Changed

- changed CLI to use a single `run` command with `--config`, `--dry-run`, and `--verbose` flags
- changed dependency management to use interface-based design for providers and updaters
- redesigned from single Azure DevOps provider to multi-provider architecture

### Removed

- removed separate `scan`, `list`, and `upgrade` CLI commands
- removed tightly coupled Azure DevOps-only implementation
