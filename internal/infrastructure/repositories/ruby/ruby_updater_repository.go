package ruby

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
	"github.com/rios0rios0/autoupdate/internal/support"
	langRuby "github.com/rios0rios0/langforge/pkg/infrastructure/languages/ruby"
)

const (
	updaterName = "ruby"
	// runtimeName is how the runtime is spelled in log lines, commit subjects
	// and pull request text.
	runtimeName      = "Ruby"
	rbVersionTimeout = 15 * time.Second
	scriptFileMode   = 0o700

	// Branch name patterns for Ruby updates. One format is used when the
	// Ruby runtime version itself is being bumped; the other is used when
	// only gem dependencies are being refreshed.
	branchRbVersionFmt = "chore/upgrade-ruby-%s"
	branchRbDepsFmt    = "chore/upgrade-ruby-deps"

	// Commit/PR messages and changelog entries used across remote and local modes.
	rbCommitMsgDeps      = "chore(deps): updated Ruby gem dependencies"
	rbChangelogEntryDeps = "- changed the Ruby gem dependencies to their latest versions"
)

// defaultRunner is the package-level command runner for remote-mode functions.
var defaultRunner cmdrunner.Runner = cmdrunner.NewDefaultRunner() //nolint:gochecknoglobals // test override

// UpdaterRepository implements repositories.UpdaterRepository for Ruby dependencies.
// It clones the repository locally, runs bundler commands to update
// dependencies, pushes the changes, and creates a PR via the provider API.
type UpdaterRepository struct {
	versionFetcher VersionFetcher
	cmdRunner      cmdrunner.Runner
}

// NewUpdaterRepository creates a new Ruby updater with default dependencies.
func NewUpdaterRepository() repositories.UpdaterRepository {
	return &UpdaterRepository{
		versionFetcher: NewHTTPRubyVersionFetcher(&http.Client{Timeout: rbVersionTimeout}),
		cmdRunner:      cmdrunner.NewDefaultRunner(),
	}
}

// NewUpdaterRepositoryWithDeps creates a Ruby updater with injected dependencies (for testing).
func NewUpdaterRepositoryWithDeps(vf VersionFetcher) repositories.UpdaterRepository {
	return &UpdaterRepository{versionFetcher: vf, cmdRunner: cmdrunner.NewDefaultRunner()}
}

func (u *UpdaterRepository) Name() string { return updaterName }

// Detect returns true if the repository has Ruby marker files (e.g. Gemfile, .ruby-version).
func (u *UpdaterRepository) Detect(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) bool {
	found, err := support.DetectRemote(ctx, &langRuby.Detector{}, provider, repo)
	if err != nil {
		logger.Warnf("[ruby] detection error for %s/%s: %v", repo.Organization, repo.Name, err)
		return false
	}
	return found
}

// CreateUpdatePRs clones the repo, upgrades Ruby dependencies,
// and creates a PR.
func (u *UpdaterRepository) CreateUpdatePRs(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
) ([]entities.PullRequest, error) {
	logger.Infof("[ruby] Processing %s/%s", repo.Organization, repo.Name)

	latestRbVersion := support.LatestVersion(
		support.VersionFeed{LogPrefix: updaterName, Runtime: runtimeName, Release: "stable Ruby"},
		func() (string, error) { return u.versionFetcher.FetchLatestVersion(ctx) },
	)

	vCtx := resolveVersionContext(ctx, provider, repo, latestRbVersion, opts.AllowMajorUpdates)

	return support.RunRemoteUpgrade(ctx, provider, repo, opts, support.RemoteUpgrade{
		LogPrefix:  updaterName,
		BranchName: vCtx.BranchName,
		DryRun:     func() { logDryRun(vCtx, repo) },
		Upgrade: func(ctx context.Context) (support.RemoteUpgradeResult, error) {
			return cloneAndUpgrade(ctx, provider, repo, vCtx, opts.AllowMajorUpdates)
		},
	})
}

// logDryRun logs what would happen without actually performing the upgrade.
func logDryRun(vCtx *versionContext, repo entities.Repository) {
	support.LogRemoteDryRun(updaterName, repo, support.DryRunPlan{
		Runtime:         runtimeName,
		Version:         vCtx.LatestVersion,
		UpgradesVersion: vCtx.NeedsVersionUpgrade,
		Dependencies:    "Ruby gem dependencies",
	})
}

// cloneAndUpgrade prepares the changelog, clones the repository, runs the
// upgrade script, and describes the pull request the upgrade earned.
func cloneAndUpgrade(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	vCtx *versionContext,
	allowMajorUpdates bool,
) (support.RemoteUpgradeResult, error) {
	changelog := support.StageRemoteChangelog(ctx, provider, repo, changelogEntries(vCtx))
	defer changelog.Remove()

	result, err := upgradeRepo(ctx, upgradeParams{
		CloneTarget: support.CloneTargetFor(provider, repo, vCtx.BranchName, changelog),
		RubyVersion: rubyVersionFor(vCtx),
	}, allowMajorUpdates)
	if err != nil {
		return support.RemoteUpgradeResult{}, fmt.Errorf("failed to upgrade: %w", err)
	}

	return support.RemoteUpgradeResult{
		Pushed: result.HasChanges,
		PullRequest: support.PullRequestSpec{
			LogPrefix:  updaterName,
			BranchName: vCtx.BranchName,
			Title:      upgradeSubject(vCtx.LatestVersion, result.RubyVersionUpdated),
			Description: GeneratePRDescription(
				vCtx.LatestVersion, result.RubyVersionUpdated, allowMajorUpdates,
			),
		},
	}, nil
}

// upgradeSubject is the one-line summary of what the run changed, used as both
// the commit subject and the pull request title.
func upgradeSubject(rbVersion string, rbVersionUpdated bool) string {
	if rbVersionUpdated {
		return fmt.Sprintf(
			"chore(deps): upgraded Ruby to `%s` and updated all gem dependencies",
			rbVersion,
		)
	}

	return rbCommitMsgDeps
}

// ApplyUpdates implements repositories.LocalUpdater. It runs language-specific
// Ruby upgrade operations on a locally cloned repository, without performing
// any git clone, branch, commit, or push operations.
func (u *UpdaterRepository) ApplyUpdates(
	ctx context.Context,
	repoDir string,
	_ repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
) (*repositories.LocalUpdateResult, error) {
	logger.Infof("[ruby] Processing local clone of %s/%s", repo.Organization, repo.Name)

	// resolveLocalVersionContext (from local.go) handles fetching + comparison
	vCtx := resolveLocalVersionContext(ctx, repoDir, opts.AllowMajorUpdates)

	env := os.Environ()
	if pinVersion := rubyVersionFor(vCtx); pinVersion != "" {
		env = append(env, "TARGET_RUBY_VERSION="+pinVersion)
	}

	outputStr, runErr := cmdrunner.RunScript(ctx, u.cmdRunner, cmdrunner.ScriptRun{
		Body:        buildBatchRubyScript(opts.AllowMajorUpdates),
		TempPattern: "autoupdate-ruby-local-*",
		Dir:         repoDir,
		Env:         env,
		LogPrefix:   updaterName,
		Verbose:     true,
	})
	if runErr != nil {
		return nil, runErr
	}

	rbVersionUpdated := strings.Contains(outputStr, "RUBY_VERSION_UPDATED=true")

	// Return early if the upgrade script made no filesystem changes
	if !support.HasUncommittedChanges(ctx, repoDir) {
		logger.Infof("[ruby] No filesystem changes detected after upgrade script")
		return nil, repositories.ErrNoUpdatesNeeded
	}

	// Record the upgrade in the repository's changelog.
	var entry string
	if rbVersionUpdated {
		entry = fmt.Sprintf(
			"- changed the Ruby version to `%s` and updated all gem dependencies",
			vCtx.LatestVersion,
		)
	} else {
		entry = rbChangelogEntryDeps
	}
	support.LocalChangelogUpdate(repoDir, []string{entry})

	commitMsg := upgradeSubject(vCtx.LatestVersion, rbVersionUpdated)

	return &repositories.LocalUpdateResult{
		BranchName:    vCtx.BranchName,
		CommitMessage: commitMsg,
		PRTitle:       commitMsg,
		PRDescription: GeneratePRDescription(vCtx.LatestVersion, rbVersionUpdated, opts.AllowMajorUpdates),
	}, nil
}

// buildBatchRubyScript generates a bash script with only language-specific
// operations (no git clone, branch, commit, or push) for the batch pipeline.
func buildBatchRubyScript(allowMajorUpdates bool) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")

	writeRubyUpgradeCommands(&sb, allowMajorUpdates)
	writeDockerfileUpdate(&sb)

	return sb.String()
}

// --- internal types ---

type versionContext struct {
	LatestVersion       string
	NeedsVersionUpgrade bool
	BranchName          string
}

type upgradeParams struct {
	// CloneTarget identifies the repository the script clones and how it
	// authenticates against it, and carries the staged changelog.
	support.CloneTarget

	RubyVersion string
}

type upgradeResult struct {
	HasChanges         bool
	RubyVersionUpdated bool
	Output             string
}

// parseRubyVersionFile extracts the Ruby version from a .ruby-version
// file content. The file typically contains just a version string like "3.3.6".
func parseRubyVersionFile(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return ""
}

// --- version context ---

// resolveVersionContext reads the remote .ruby-version to find the current
// Ruby version and picks the right branch-name pattern.
func resolveVersionContext(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	latestRbVersion string,
	allowMajorUpdates bool,
) *versionContext {
	needsVersionUpgrade := false

	if latestRbVersion != "" && provider.HasFile(ctx, repo, ".ruby-version") {
		content, err := provider.GetFileContent(ctx, repo, ".ruby-version")
		if err == nil {
			currentVersion := parseRubyVersionFile(content)
			needsVersionUpgrade = support.AcceptsUpgrade(
				currentVersion, latestRbVersion, allowMajorUpdates,
			)
			logger.Infof(
				"[ruby] Current .ruby-version: %s (upgrade needed: %v)",
				currentVersion, needsVersionUpgrade,
			)
		}
	}

	branchName := branchRbDepsFmt
	if needsVersionUpgrade {
		branchName = fmt.Sprintf(branchRbVersionFmt, latestRbVersion)
	}

	return &versionContext{
		LatestVersion:       latestRbVersion,
		NeedsVersionUpgrade: needsVersionUpgrade,
		BranchName:          branchName,
	}
}

// changelogEntries renders the Keep a Changelog bullet describing the
// upgrade. The staging helpers turn it into a chlog fragment when the
// target repository uses that format instead.
func changelogEntries(vCtx *versionContext) []string {
	if vCtx.NeedsVersionUpgrade {
		return []string{fmt.Sprintf(
			"- changed the Ruby version to `%s` and updated all gem dependencies",
			vCtx.LatestVersion,
		)}
	}
	return []string{rbChangelogEntryDeps}
}

// --- clone + upgrade ---

func upgradeRepo(
	ctx context.Context,
	params upgradeParams,
	allowMajorUpdates bool,
) (*upgradeResult, error) {
	output, err := cmdrunner.RunCloneScript(ctx, defaultRunner, cmdrunner.CloneScriptRun{
		Body:        buildUpgradeScript(params, "", allowMajorUpdates),
		TempPattern: "autoupdate-ruby-*",
		Env:         func(repoDir string) []string { return buildEnv(params, repoDir) },
		Secrets:     []string{params.AuthToken},
	})
	if err != nil {
		return nil, err
	}

	return &upgradeResult{
		HasChanges:         strings.Contains(output, "CHANGES_PUSHED=true"),
		RubyVersionUpdated: strings.Contains(output, "RUBY_VERSION_UPDATED=true"),
		Output:             output,
	}, nil
}

func buildUpgradeScript(
	params upgradeParams,
	repoDir string,
	allowMajorUpdates bool,
) string {
	_ = repoDir // used via env vars in the script

	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")

	// Set up git credentials based on provider, then clone onto the branch.
	writeGitAuth(&sb, params)
	sb.WriteString(support.RemoteCloneScript())

	// Ruby upgrade commands
	writeRubyUpgradeCommands(&sb, allowMajorUpdates)

	// Update Dockerfile ruby image tags
	writeDockerfileUpdate(&sb)

	// Copy in the staged changelog: an edited CHANGELOG.md, or a chlog
	// fragment when the target repository uses that format.
	sb.WriteString(support.ChangelogUpdateScript())

	// Check for changes and commit/push
	writeCommitAndPush(&sb)

	return sb.String()
}

func writeGitAuth(sb *strings.Builder, params upgradeParams) {
	sb.WriteString(support.GitAuthScript(params.ProviderName))
}

// writeRubyUpgradeCommands emits the Ruby pin rewrite and the gem upgrade.
//
// The two directions of allow_major_updates are expressed differently, because
// bundler resolves only inside the bounds the Gemfile declares and has no flag
// meaning "raise them". Refusing uses bundler's own ceiling: `bundle update
// --minor` caps the bump below a major whatever the Gemfile permits, which is
// the direction that was missing first -- an unconstrained `gem "rails"`
// crossed majors however the key was set. Allowing needs the manifest edited,
// which support.GemfileConstraintScript does in two moves around the
// resolution: every pessimistic (`~>`) constraint on a `gem` line is widened to
// `>=` *before* `bundle update`, so the resolution sees the raised bounds, and
// each one is re-tightened afterwards to `~>` the version that resolved, at the
// precision the repository wrote -- `~> 6.0` becomes `~> 7.1`, never `>= 6.0`
// for ever -- and the lockfile is re-locked against the raised bounds, since
// the resolution recorded the widened ones. A constraint whose gem stayed
// inside its bound is put back untouched. When the widened resolution fails, the manifest is restored and
// `bundle update` retried within the bounds the repository declared, so a
// conflict between two new majors degrades to the smaller upgrade rather than
// to a dropped ceiling beside a stale lockfile, or to no upgrade at all. Only
// the `Gemfile` is touched, never a `.gemspec`.
func writeRubyUpgradeCommands(sb *strings.Builder, allowMajorUpdates bool) {
	// The guard also declines every non-numeric pin, so a repository running
	// JRuby or TruffleRuby is no longer handed an MRI version number.
	sb.WriteString(support.VersionPinUpdateScript(support.VersionPinUpdate{
		File:       ".ruby-version",
		Subject:    runtimeName,
		VersionVar: "TARGET_RUBY_VERSION",
		CurrentVar: "CURRENT_RB_VERSION",
		ChangedVar: "RUBY_VERSION_CHANGED",
		MarkerVar:  "RUBY_VERSION",
	}))

	// Update bundler and bundle update
	sb.WriteString("# Update bundler and gem dependencies\n")
	if allowMajorUpdates {
		sb.WriteString(support.GemfileConstraintScript())
	}
	sb.WriteString("if [ -f \"Gemfile\" ]; then\n")
	sb.WriteString("    echo \"Updating bundler...\"\n")
	sb.WriteString("    gem update bundler 2>&1 || echo \"WARNING: gem update bundler had some errors\"\n\n")
	if allowMajorUpdates {
		writeGemfileWideningUpdate(sb)
	} else {
		// --major is bundler's default, so the allowing direction never spells
		// it out; --minor is the cap.
		sb.WriteString("    echo \"Running bundle update --minor...\"\n")
		sb.WriteString("    bundle update --minor 2>&1 || echo \"WARNING: bundle update had some errors\"\n")
	}
	sb.WriteString("fi\n\n")
}

// writeGemfileWideningUpdate emits the allowing direction: widen, resolve,
// re-tighten, re-lock -- or, when the widened resolution fails, restore the
// manifest and resolve within the bounds it declared, so the run still carries
// every upgrade bundler can make. The widening comes before `bundle update`
// because editing the manifest after the resolution would leave the lockfile
// resolved against the ceiling that was just removed, which is the worst of
// both. The re-lock comes after the re-tightening for the mirror-image reason:
// `bundle update` ran against the widened manifest, so the lockfile's
// DEPENDENCIES block records `rails (>= 6.0)` beside a Gemfile that now says
// `~> 7.1`, and a frozen or deployment install refuses that mismatch with "the
// dependencies in your gemfile changed". `bundle lock` is the conservative
// reconcile: every locked version already satisfies the raised bound, so it
// rewrites DEPENDENCIES and moves nothing.
func writeGemfileWideningUpdate(sb *strings.Builder) {
	sb.WriteString("    autoupdate_relax_gemfile_constraints Gemfile\n")
	sb.WriteString("    echo \"Running bundle update...\"\n")
	sb.WriteString("    if bundle update 2>&1; then\n")
	sb.WriteString("        autoupdate_retighten_gemfile_constraints Gemfile Gemfile.lock\n")
	sb.WriteString(
		"        bundle lock 2>&1 || " +
			"echo \"WARNING: could not re-lock Gemfile.lock against the raised constraints\"\n",
	)
	sb.WriteString("    else\n")
	sb.WriteString(
		"        echo \"WARNING: bundle update could not resolve past the declared bounds, " +
			"retrying within them\"\n",
	)
	sb.WriteString("        autoupdate_restore_gemfile_constraints Gemfile\n")
	sb.WriteString("        bundle update 2>&1 || echo \"WARNING: bundle update had some errors\"\n")
	sb.WriteString("    fi\n")
}

func writeDockerfileUpdate(sb *strings.Builder) {
	sb.WriteString(support.DockerfileTagUpdateScript(support.DockerfileTagUpdate{
		ChangedVar: "RUBY_VERSION_CHANGED",
		VersionVar: "TARGET_RUBY_VERSION",
		Subject:    runtimeName,
		Images:     []support.DockerfileImage{{Name: "ruby"}},
	}))
}

func writeCommitAndPush(sb *strings.Builder) {
	sb.WriteString(support.CommitAndPushScript(support.CommitAndPush{
		UpgradedWhen: `[ "$RUBY_VERSION_CHANGED" = "true" ]`,
		UpgradeMessage: "chore(deps): upgraded Ruby to `$TARGET_RUBY_VERSION` " +
			"and updated all gem dependencies",
		DepsMessage: rbCommitMsgDeps,
	}))
}

func buildEnv(params upgradeParams, repoDir string) []string {
	env := support.CloneEnv(params.CloneTarget, repoDir)
	if params.RubyVersion != "" {
		env = append(env, "TARGET_RUBY_VERSION="+params.RubyVersion)
	}

	return env
}

// GeneratePRDescription builds a markdown PR description for a Ruby
// dependency upgrade. Exported so that the local-mode CLI handler can
// reuse the same description format.
//
// The wording follows the flag the script was actually given. With majors
// allowed the diff can carry a `Gemfile` edit -- the raised `~>` bounds -- and
// a reviewer pointed only at `Gemfile.lock` would merge a constraint change
// without being told to look at it; with majors refused only the lockfile
// moves, and claiming otherwise would send the reviewer after changes that are
// not there.
func GeneratePRDescription(rbVersion string, rbVersionUpdated, allowMajorUpdates bool) string {
	var sb strings.Builder
	sb.WriteString("## Summary\n\n")
	if rbVersionUpdated {
		sb.WriteString(
			"This PR upgrades the Ruby version to **" + rbVersion + "** and updates all gem dependencies.\n\n",
		)
	} else {
		sb.WriteString(
			"This PR updates all Ruby gem dependencies to their latest versions.\n\n",
		)
	}
	sb.WriteString("### Changes\n\n")
	if rbVersionUpdated {
		sb.WriteString("- Updated `.ruby-version` to `" + rbVersion + "`\n")
	}
	sb.WriteString("- Ran `gem update bundler` to ensure bundler is current\n")
	if allowMajorUpdates {
		sb.WriteString(
			"- Ran `bundle update` with the pessimistic (`~>`) constraints in `Gemfile` widened, " +
				"then raised each constraint whose gem resolved past its bound to the new version " +
				"(`~> 6.0` becomes `~> 7.1`); constraints whose gems stayed within their bounds are " +
				"unchanged\n",
		)
	} else {
		sb.WriteString(
			"- Ran `bundle update --minor`, which re-resolves `Gemfile.lock` within the current " +
				"major of every gem (`allow_major_updates` is off for this repository)\n",
		)
	}
	sb.WriteString("\n### Review Checklist\n\n")
	sb.WriteString("- [ ] Verify build passes\n")
	sb.WriteString("- [ ] Verify tests pass\n")
	sb.WriteString("- [ ] Review dependency changes in `Gemfile.lock`\n")
	if allowMajorUpdates {
		sb.WriteString("- [ ] Review the constraint changes in `Gemfile` for breaking major bumps\n")
	}
	sb.WriteString("\n---\n")
	sb.WriteString("*This PR was automatically created by [autoupdate](https://github.com/rios0rios0/autoupdate)*\n")
	return sb.String()
}

// rubyVersionFor returns the version the generated script may write into the pin, which
// is the empty string whenever this run declined the upgrade.
//
// The decision is made in Go and the rewrite happens in bash, and the script's
// own gate -- autoupdate_version_is_newer -- has no major-version concept. So
// the refusal has to be expressed by withholding the value rather than by
// trusting the script to re-derive it: otherwise `allow_major_updates: false`
// leaves NeedsVersionUpgrade false (no branch name, commit message, changelog
// entry or PR title mentioning a version bump) while the script rewrites the pin
// across the major anyway. That is worse than not gating at all, because the two
// halves used to agree.
//
// The same shape as dart's sdkVersionFor, for the same reason.
func rubyVersionFor(vCtx *versionContext) string {
	if vCtx.NeedsVersionUpgrade {
		return vCtx.LatestVersion
	}

	return ""
}
