package terraform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	logger "github.com/sirupsen/logrus"
	"github.com/zclconf/go-cty/cty"
	"golang.org/x/mod/semver"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/ociregistry"
	"github.com/rios0rios0/autoupdate/internal/support"
	langTerraform "github.com/rios0rios0/langforge/pkg/infrastructure/languages/terraform"
)

const (
	updaterName         = "terraform"
	minMatchLen         = 6
	maxDetailedUpgrades = 5
	branchBatchFmt      = "chore/upgrade-%d-dependencies"
	branchSingleFmt     = "chore/upgrade-%s-%s"

	// terraformBlockModule is the HCL block type used to declare a Terraform
	// module. Extracted as a constant to avoid repeated string literals (goconst).
	terraformBlockModule = "module"
)

// depKind distinguishes Terraform module references (in .tf files) from
// container image references (in .hcl / Terragrunt files).
type depKind int

const (
	depKindModule depKind = iota
	depKindImage
)

// UpdaterRepository implements repositories.UpdaterRepository for Terraform module dependencies.
// It reads files via the provider API, detects version refs, and creates PRs
// with updated version strings — no local clone required.
type UpdaterRepository struct {
	// registry is asked which tags a container registry holds, for the image pins of the
	// files an image_registries rule covers. Nil means no registry can be asked, and a pin
	// such a rule covers then stays where it is.
	registry repositories.ImageRegistryRepository
}

// NewUpdaterRepository creates a new Terraform updater, which asks container registries
// with the credentials the Docker configuration holds.
func NewUpdaterRepository() repositories.UpdaterRepository {
	return &UpdaterRepository{registry: ociregistry.NewRegistryRepository()}
}

// NewUpdaterRepositoryWithDeps creates a Terraform updater that asks registries through the
// given client (for testing).
func NewUpdaterRepositoryWithDeps(registry repositories.ImageRegistryRepository) repositories.UpdaterRepository {
	return &UpdaterRepository{registry: registry}
}

func (u *UpdaterRepository) Name() string { return updaterName }

// Detect returns true if the repository contains Terraform marker files (e.g. *.tf, *.hcl).
func (u *UpdaterRepository) Detect(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) bool {
	found, err := support.DetectRemote(ctx, &langTerraform.Detector{}, provider, repo)
	if err != nil {
		logger.Warnf("[terraform] detection error for %s/%s: %v", repo.Organization, repo.Name, err)
		return false
	}
	return found
}

// CreateUpdatePRs scans for outdated Terraform module dependencies,
// groups upgrades by repository, and creates PRs with the changes.
func (u *UpdaterRepository) CreateUpdatePRs(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
) ([]entities.PullRequest, error) {
	logger.Infof(
		"[terraform] Scanning %s/%s for Terraform dependencies",
		repo.Organization, repo.Name,
	)

	allDeps := u.scanAllDependencies(ctx, provider, repo)
	if len(allDeps) == 0 {
		return []entities.PullRequest{}, nil
	}

	plan := u.planUpgrades(ctx, provider, repo, allDeps, opts)
	logHeldUpgrades(repo, plan.held)
	if len(plan.upgrades) == 0 {
		logger.Infof(
			"[terraform] %s/%s: all Terraform dependencies up to date",
			repo.Organization, repo.Name,
		)
		return []entities.PullRequest{}, nil
	}

	logger.Infof(
		"[terraform] %s/%s: found %d dependencies to upgrade",
		repo.Organization, repo.Name, len(plan.upgrades),
	)

	if opts.DryRun {
		for _, up := range plan.upgrades {
			logger.Infof(
				"[terraform] [DRY RUN] Would upgrade %s: %s -> %s",
				extractRepoName(up.dep.Source), up.dep.CurrentVer, up.newVersion,
			)
		}
		return []entities.PullRequest{}, nil
	}

	return u.createUpgradePR(ctx, provider, repo, opts, plan)
}

// ApplyUpdates implements repositories.LocalUpdater for the clone-based pipeline.
// It scans the local filesystem for Terraform dependencies, determines upgrades
// using the provider API for tag resolution, writes changes to disk, and returns
// the PR metadata.
func (u *UpdaterRepository) ApplyUpdates(
	ctx context.Context,
	repoDir string,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
) (*repositories.LocalUpdateResult, error) {
	logger.Infof("[terraform] Scanning local clone of %s/%s for Terraform dependencies",
		repo.Organization, repo.Name)

	allDeps := u.localScanAllDependencies(repoDir)
	if len(allDeps) == 0 {
		return nil, repositories.ErrNoUpdatesNeeded
	}

	plan := u.planUpgrades(ctx, provider, repo, allDeps, opts)
	logHeldUpgrades(repo, plan.held)
	upgrades := plan.upgrades
	if len(upgrades) == 0 {
		return nil, repositories.ErrNoUpdatesNeeded
	}

	logger.Infof("[terraform] %s/%s: found %d dependencies to upgrade (local)",
		repo.Organization, repo.Name, len(upgrades))

	fileChanges := applyUpgrades(upgrades)
	if err := support.WriteFileChanges(repoDir, fileChanges); err != nil {
		return nil, err
	}

	support.LocalDependencyChangelogUpdate(repoDir, dependencyChanges(upgrades))

	return &repositories.LocalUpdateResult{
		BranchName:    generateBranchName(upgrades),
		CommitMessage: generateCommitMessage(upgrades),
		PRTitle:       generatePRTitle(upgrades),
		PRDescription: generatePRDescription(upgrades, plan.held),
	}, nil
}

// localScanAllDependencies walks the local filesystem for .tf and .hcl files
// and parses them for module and container image dependencies.
func (u *UpdaterRepository) localScanAllDependencies(repoDir string) []depWithContent {
	var allDeps []depWithContent

	tfFiles, err := support.WalkFilesByExtension(repoDir, ".tf")
	if err != nil {
		logger.Warnf("[terraform] Failed to walk .tf files: %v", err)
	}
	for _, relPath := range tfFiles {
		data, readErr := os.ReadFile(filepath.Join(repoDir, relPath))
		if readErr != nil {
			logger.Warnf("[terraform] Failed to read %s: %v", relPath, readErr)
			continue
		}
		content := string(data)
		deps := scanTerraformFile(content, relPath)
		for _, dep := range deps {
			allDeps = append(allDeps, depWithContent{
				Dependency:  dep,
				FileContent: content,
				Kind:        depKindModule,
			})
		}
	}

	hclFiles, hclErr := support.WalkFilesByExtension(repoDir, ".hcl")
	if hclErr != nil {
		logger.Warnf("[terraform] Failed to walk .hcl files: %v", hclErr)
	}
	for _, relPath := range hclFiles {
		data, readErr := os.ReadFile(filepath.Join(repoDir, relPath))
		if readErr != nil {
			logger.Warnf("[terraform] Failed to read %s: %v", relPath, readErr)
			continue
		}
		content := string(data)
		deps := scanHCLFile(content, relPath)
		for _, dep := range deps {
			allDeps = append(allDeps, depWithContent{
				Dependency:  dep,
				FileContent: content,
				Kind:        depKindImage,
			})
		}
	}

	return allDeps
}

// scanAllDependencies lists .tf and .hcl files and parses them for
// module dependencies (from .tf) and container image references (from .hcl).
func (u *UpdaterRepository) scanAllDependencies(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) []depWithContent {
	var allDeps []depWithContent

	// Scan .tf files for Terraform module references
	tfFiles, err := provider.ListFiles(ctx, repo, ".tf")
	if err != nil {
		logger.Warnf("[terraform] Failed to list .tf files: %v", err)
	}

	for _, f := range tfFiles {
		if f.IsDir {
			continue
		}
		content, contentErr := provider.GetFileContent(ctx, repo, f.Path)
		if contentErr != nil {
			logger.Warnf("[terraform] Failed to read %s: %v", f.Path, contentErr)
			continue
		}

		deps := scanTerraformFile(content, f.Path)
		for _, dep := range deps {
			allDeps = append(allDeps, depWithContent{
				Dependency:  dep,
				FileContent: content,
				Kind:        depKindModule,
			})
		}
	}

	// Scan .hcl files for container image references (Terragrunt)
	hclFiles, hclErr := provider.ListFiles(ctx, repo, ".hcl")
	if hclErr != nil {
		logger.Warnf("[terraform] Failed to list .hcl files: %v", hclErr)
	}

	for _, f := range hclFiles {
		if f.IsDir {
			continue
		}
		content, contentErr := provider.GetFileContent(ctx, repo, f.Path)
		if contentErr != nil {
			logger.Warnf("[terraform] Failed to read %s: %v", f.Path, contentErr)
			continue
		}

		deps := scanHCLFile(content, f.Path)
		for _, dep := range deps {
			allDeps = append(allDeps, depWithContent{
				Dependency:  dep,
				FileContent: content,
				Kind:        depKindImage,
			})
		}
	}

	return allDeps
}

// upgradePlan is what a scan decided: the pins to move, and the newer image tags left out.
type upgradePlan struct {
	upgrades []upgradeTask
	held     []heldUpgrade
}

// planUpgrades resolves each dependency's tags and decides which pins move, and where.
//
// A module moves to the newest released tag, as it always has. An image pin moves to the
// newest tag the imagePlanner accepts, and the newer tags it does not accept are kept in the
// plan so the log and the pull request can say so.
func (u *UpdaterRepository) planUpgrades(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	allDeps []depWithContent,
	opts entities.UpdateOptions,
) upgradePlan {
	resolver := newSourceResolver(provider, repo)
	images := newImagePlanner(u.registry, opts.ImageRegistries, repo)
	sources := make(map[sourceKey]resolvedSource)

	var plan upgradePlan
	for _, dc := range allDeps {
		key := sourceKey{source: dc.Dependency.Source, kind: dc.Kind}
		resolved, seen := sources[key]
		if !seen {
			resolved = resolveSource(ctx, provider, resolver, key)
			sources[key] = resolved
		}
		if len(resolved.tags) == 0 {
			continue
		}

		var newVersion string
		if dc.Kind == depKindImage {
			var held []heldUpgrade
			newVersion, held = images.choose(ctx, dc.Dependency, resolved.released)
			plan.held = append(plan.held, held...)
		} else if isNewerVersion(dc.Dependency.CurrentVer, resolved.latestVersion, opts.AllowMajorUpdates) {
			newVersion = resolved.latestVersion
		}
		if newVersion == "" || newVersion == dc.Dependency.CurrentVer {
			continue
		}

		plan.upgrades = append(plan.upgrades, upgradeTask{
			dep:         dc.Dependency,
			newVersion:  newVersion,
			fileContent: dc.FileContent,
			kind:        dc.Kind,
		})
	}

	return plan
}

// resolveSource reads the tags of the repository a source names and works out which of them
// are released.
func resolveSource(
	ctx context.Context,
	provider repositories.ProviderRepository,
	resolver *sourceResolver,
	key sourceKey,
) resolvedSource {
	tags, depRepo := resolver.resolve(ctx, key)
	if len(tags) == 0 {
		return resolvedSource{tags: nil, depRepo: depRepo, released: nil, latestVersion: ""}
	}

	released := releasedTags(ctx, provider, depRepo, tags)
	return resolvedSource{tags: tags, depRepo: depRepo, released: released, latestVersion: released[0]}
}

// createUpgradePR creates a branch with changes and opens a PR.
func (u *UpdaterRepository) createUpgradePR(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
	plan upgradePlan,
) ([]entities.PullRequest, error) {
	upgrades := plan.upgrades
	branchName := generateBranchName(upgrades)
	if support.PullRequestOpen(ctx, provider, repo, updaterName, branchName) {
		return []entities.PullRequest{}, nil
	}

	fileChanges := applyUpgrades(upgrades)
	fileChanges = appendChangelogEntry(ctx, provider, repo, upgrades, fileChanges)

	return support.PushChangesAndOpenPullRequest(ctx, provider, repo, opts, support.BranchPullRequest{
		LogPrefix:     updaterName,
		BranchName:    branchName,
		Title:         generatePRTitle(upgrades),
		Description:   generatePRDescription(upgrades, plan.held),
		CommitMessage: generateCommitMessage(upgrades),
		Changes:       fileChanges,
	})
}

// --- internal types ---

type depWithContent struct {
	Dependency  entities.Dependency
	FileContent string
	Kind        depKind
}

type upgradeTask struct {
	dep         entities.Dependency
	newVersion  string
	fileContent string
	kind        depKind
}

// --- scanning ---

func scanTerraformFile(content, filePath string) []entities.Dependency {
	parser := hclparse.NewParser()

	file, diags := parser.ParseHCL([]byte(content), filePath)
	if diags.HasErrors() || file == nil {
		return scanWithRegex(content, filePath)
	}

	body := file.Body
	if body == nil {
		return nil
	}

	bodyContent, _, partialDiags := body.PartialContent(&hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: terraformBlockModule, LabelNames: []string{"name"}},
		},
	})
	if partialDiags.HasErrors() {
		return scanWithRegex(content, filePath)
	}

	var deps []entities.Dependency
	for _, block := range bodyContent.Blocks {
		if block.Type != terraformBlockModule {
			continue
		}

		moduleName := ""
		if len(block.Labels) > 0 {
			moduleName = block.Labels[0]
		}

		attrs, _ := block.Body.JustAttributes()
		sourceAttr, hasSource := attrs["source"]
		if !hasSource {
			continue
		}

		sourceVal, sourceDiags := sourceAttr.Expr.Value(&hcl.EvalContext{})
		if sourceDiags.HasErrors() || sourceVal.Type() != cty.String {
			continue
		}

		source := sourceVal.AsString()
		if !isGitModule(source) {
			continue
		}

		version := extractVersion(source)
		if version == "" {
			continue
		}

		cleanSource := removeVersionFromSource(source)
		deps = append(deps, entities.Dependency{
			Name:       moduleName,
			Source:     cleanSource,
			CurrentVer: version,
			FilePath:   filePath,
			Line:       block.DefRange.Start.Line,
		})
	}

	return deps
}

func scanWithRegex(content, filePath string) []entities.Dependency {
	var deps []entities.Dependency

	modulePattern := regexp.MustCompile(
		`(?s)module\s+"([^"]+)"\s*\{[^}]*source\s*=\s*"([^"]+)"`,
	)
	matches := modulePattern.FindAllStringSubmatchIndex(content, -1)

	for _, match := range matches {
		if len(match) < minMatchLen {
			continue
		}

		moduleName := content[match[2]:match[3]]
		source := content[match[4]:match[5]]

		if !isGitModule(source) {
			continue
		}

		version := extractVersion(source)
		if version == "" {
			continue
		}

		cleanSource := removeVersionFromSource(source)
		lineNum := strings.Count(content[:match[0]], "\n") + 1

		deps = append(deps, entities.Dependency{
			Name:       moduleName,
			Source:     cleanSource,
			CurrentVer: version,
			FilePath:   filePath,
			Line:       lineNum,
		})
	}

	return deps
}

// scanHCLFile parses a Terragrunt .hcl file for container image references.
// It detects patterns like: relayer_http_image = "relayer-http:0.7.0"
// where the image name corresponds to a repository in the same organisation
// and the tag after the colon is a Git tag / semver version.
func scanHCLFile(content, filePath string) []entities.Dependency {
	var deps []entities.Dependency

	imagePattern := regexp.MustCompile(
		`(\w+_image)\s*=\s*"([a-zA-Z0-9][a-zA-Z0-9._-]*):([^"]+)"`,
	)
	matches := imagePattern.FindAllStringSubmatch(content, -1)
	matchIndices := imagePattern.FindAllStringIndex(content, -1)

	for i, match := range matches {
		varName := match[1]
		imageName := match[2]
		version := match[3]

		// Skip non-semver tags like "latest"
		if !isSemverLike(version) {
			continue
		}

		lineNum := strings.Count(content[:matchIndices[i][0]], "\n") + 1

		deps = append(deps, entities.Dependency{
			Name:       varName,
			Source:     imageName,
			CurrentVer: version,
			FilePath:   filePath,
			Line:       lineNum,
		})
	}

	return deps
}

// isSemverLike returns true if the version string looks like a semantic
// version (e.g. "1.2.3", "v0.7.0"). It rejects tags like "latest".
func isSemverLike(version string) bool {
	return semver.IsValid(normalizeVersion(version))
}

// --- source helpers ---

func isGitModule(source string) bool {
	return strings.HasPrefix(source, "git::") ||
		strings.HasPrefix(source, "git@") ||
		strings.Contains(source, "github.com") ||
		strings.Contains(source, "gitlab.com") ||
		strings.Contains(source, "bitbucket.org") ||
		strings.Contains(source, "dev.azure.com") ||
		strings.Contains(source, "_git/")
}

func extractVersion(source string) string {
	refPattern := regexp.MustCompile(`\?ref=([^&\s]+)`)
	if matches := refPattern.FindStringSubmatch(source); len(matches) > 1 {
		return matches[1]
	}
	refPattern2 := regexp.MustCompile(`ref=([^&\s"]+)`)
	if matches := refPattern2.FindStringSubmatch(source); len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func removeVersionFromSource(source string) string {
	refPattern := regexp.MustCompile(`\?ref=[^&\s"]+`)
	return refPattern.ReplaceAllString(source, "")
}

func extractRepoName(source string) string {
	parts := strings.Split(source, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return source
}

// --- version helpers ---

// isNewerVersion reports whether newVersion is an upgrade this run may take.
//
// allowMajorUpdates is threaded in rather than defaulted: a provider or module
// constraint crossing a major is exactly what `allow_major_updates: false` is
// for, and this is the only place terraform decides it.
//
// The non-semver fallback compares strings, which cannot answer the major
// question at all, so a repository that refuses majors also refuses those --
// leaving a pin alone is the safe direction when the comparison is unreliable.
func isNewerVersion(current, newVersion string, allowMajorUpdates bool) bool {
	cur := normalizeVersion(current)
	nv := normalizeVersion(newVersion)
	if semver.IsValid(cur) && semver.IsValid(nv) {
		if semver.Compare(nv, cur) <= 0 {
			return false
		}

		return allowMajorUpdates || semver.Major(nv) == semver.Major(cur)
	}

	return allowMajorUpdates && newVersion > current
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}

// --- upgrade application ---

func applyUpgrades(tasks []upgradeTask) []entities.FileChange {
	// Group by file path
	fileContent := make(map[string]string)
	for _, t := range tasks {
		if _, ok := fileContent[t.dep.FilePath]; !ok {
			fileContent[t.dep.FilePath] = t.fileContent
		}
	}

	// Apply each upgrade to the file content, dispatching by dependency kind
	for _, t := range tasks {
		content := fileContent[t.dep.FilePath]
		if t.kind == depKindImage {
			content = applyImageVersionUpgrade(content, t.dep, t.newVersion)
		} else {
			content = applyVersionUpgrade(content, t.dep, t.newVersion)
		}
		fileContent[t.dep.FilePath] = content
	}

	var changes []entities.FileChange
	for path, content := range fileContent {
		changes = append(changes, entities.FileChange{
			Path:       path,
			Content:    content,
			ChangeType: "edit",
		})
	}
	return changes
}

func applyVersionUpgrade(
	content string,
	dep entities.Dependency,
	newVersion string,
) string {
	oldSource := buildSourceWithVersion(dep.Source, dep.CurrentVer)
	newSource := buildSourceWithVersion(dep.Source, newVersion)
	if strings.Contains(content, oldSource) {
		return strings.Replace(content, oldSource, newSource, 1)
	}

	// Regex-based fallback
	pattern := regexp.MustCompile(
		`(module\s+"` + regexp.QuoteMeta(dep.Name) +
			`"\s*\{[^}]*source\s*=\s*"[^"]*\?ref=)` +
			regexp.QuoteMeta(dep.CurrentVer) + `([^"]*")`,
	)
	if pattern.MatchString(content) {
		return pattern.ReplaceAllString(content, "${1}"+newVersion+"${2}")
	}

	refPattern := regexp.MustCompile(
		`(\?ref=)` + regexp.QuoteMeta(dep.CurrentVer) + `([^&"\s]*)`,
	)
	return refPattern.ReplaceAllStringFunc(content, func(match string) string {
		return strings.Replace(match, dep.CurrentVer, newVersion, 1)
	})
}

// applyImageVersionUpgrade replaces a container image version reference
// in a Terragrunt .hcl file. The format is "image-name:oldVersion" →
// "image-name:newVersion".
func applyImageVersionUpgrade(
	content string,
	dep entities.Dependency,
	newVersion string,
) string {
	old := dep.Source + ":" + dep.CurrentVer
	replacement := dep.Source + ":" + newVersion
	if strings.Contains(content, old) {
		return strings.Replace(content, old, replacement, 1)
	}

	// Regex fallback: match the variable assignment pattern
	pattern := regexp.MustCompile(
		`(` + regexp.QuoteMeta(dep.Name) + `\s*=\s*"` +
			regexp.QuoteMeta(dep.Source) + `:)` +
			regexp.QuoteMeta(dep.CurrentVer) + `(")`,
	)
	return pattern.ReplaceAllString(content, "${1}"+newVersion+"${2}")
}

func buildSourceWithVersion(source, version string) string {
	if strings.Contains(source, "?ref=") {
		pattern := regexp.MustCompile(`\?ref=[^&"\s]+`)
		return pattern.ReplaceAllString(source, "?ref="+version)
	}
	if strings.Contains(source, "?") {
		return source + "&ref=" + version
	}
	return source + "?ref=" + version
}

// --- tag resolution ---

// resolvedSource holds the tags, the repository entity, and the changelog-validated
// versions for a dependency source: every released tag, newest first as the provider
// sorted them, and the first of them. They are computed once per unique source to avoid
// repeated CHANGELOG API calls.
type resolvedSource struct {
	tags          []string
	depRepo       *entities.Repository
	released      []string
	latestVersion string
}

// resolveTagsForSource returns the tags of the repository a module source names, and that
// repository: the first one in the organization carrying the source's last path segment.
func resolveTagsForSource(
	ctx context.Context,
	provider repositories.ProviderRepository,
	currentRepo entities.Repository,
	source string,
) ([]string, *entities.Repository) {
	return newSourceResolver(provider, currentRepo).resolve(ctx, sourceKey{source: source, kind: depKindModule})
}

// versionHeadingRegex matches Keep-a-Changelog version headings like ## [1.2.3].
var versionHeadingRegex = regexp.MustCompile(`(?m)^\s*##\s*\[([^\]]+)\]`)

// stripVersionPrefix removes a leading "v" or "V" prefix from a version string
// so that tags like "v1.2.3" and changelog headings like "1.2.3" can be
// compared consistently.
func stripVersionPrefix(v string) string {
	return strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
}

// extractChangelogVersions parses a CHANGELOG.md and returns the set of
// normalized version strings that appear as release headings (e.g., ## [1.2.3]).
func extractChangelogVersions(content string) map[string]bool {
	versions := make(map[string]bool)
	for _, match := range versionHeadingRegex.FindAllStringSubmatch(content, -1) {
		if match[1] != "Unreleased" {
			versions[stripVersionPrefix(match[1])] = true
		}
	}
	return versions
}

// findLatestChangelogVersion iterates through tags (sorted descending by
// semver) and returns the first one documented in the dependency repo's
// CHANGELOG.md. Falls back to tags[0] when no changelog exists or no tags
// match, to avoid breaking repos that don't follow the changelog convention.
func findLatestChangelogVersion(
	ctx context.Context,
	provider repositories.ProviderRepository,
	depRepo *entities.Repository,
	tags []string,
) string {
	return releasedTags(ctx, provider, depRepo, tags)[0]
}

// releasedTags returns, in their order, the tags the dependency repo's CHANGELOG.md
// documents as releases. It returns every tag when there is no changelog, it lists no
// version, or no tag matches one, to avoid breaking repos that don't follow the changelog
// convention. tags must not be empty, and neither is the result.
func releasedTags(
	ctx context.Context,
	provider repositories.ProviderRepository,
	depRepo *entities.Repository,
	tags []string,
) []string {
	if depRepo == nil || !provider.HasFile(ctx, *depRepo, "CHANGELOG.md") {
		return tags
	}

	content, err := provider.GetFileContent(ctx, *depRepo, "CHANGELOG.md")
	if err != nil {
		logger.Warnf("[terraform] Failed to read CHANGELOG.md from %s: %v", depRepo.Name, err)
		return tags
	}

	versions := extractChangelogVersions(content)
	if len(versions) == 0 {
		return tags
	}

	var released []string
	for _, tag := range tags {
		if versions[stripVersionPrefix(tag)] {
			released = append(released, tag)
		}
	}
	if len(released) > 0 {
		return released
	}

	logger.Warnf("[terraform] No tags from %s match any CHANGELOG version, using latest tag", depRepo.Name)
	return tags
}

// --- PR text generation ---

func generateBranchName(tasks []upgradeTask) string {
	if len(tasks) == 1 {
		return fmt.Sprintf(
			branchSingleFmt,
			extractRepoName(tasks[0].dep.Source),
			tasks[0].newVersion,
		)
	}
	return fmt.Sprintf(branchBatchFmt, len(tasks))
}

func generateCommitMessage(tasks []upgradeTask) string {
	if len(tasks) == 1 {
		return fmt.Sprintf(
			"chore(deps): upgraded `%s` from `%s` to `%s`",
			extractRepoName(tasks[0].dep.Source),
			tasks[0].dep.CurrentVer,
			tasks[0].newVersion,
		)
	}
	return fmt.Sprintf(
		"chore(deps): upgraded %d Terraform dependencies",
		len(tasks),
	)
}

func generatePRTitle(tasks []upgradeTask) string {
	if len(tasks) == 1 {
		return fmt.Sprintf(
			"chore(deps): updated `%s` to `%s`",
			extractRepoName(tasks[0].dep.Source),
			tasks[0].newVersion,
		)
	}
	return fmt.Sprintf(
		"chore(deps): updated %d Terraform dependencies",
		len(tasks),
	)
}

// appendChangelogEntry records the Terraform module upgrades in the target
// repo's changelog and appends the resulting file to the change set. The
// format -- Keep a Changelog or chlog fragments -- is decided by the shared
// helper from what the repository itself commits.
func appendChangelogEntry(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	upgrades []upgradeTask,
	fileChanges []entities.FileChange,
) []entities.FileChange {
	return support.RemoteChangelogChanges(
		ctx, provider, repo, changelogEntries(upgrades), fileChanges)
}

// changelogEntries renders one Keep a Changelog bullet per upgrade.
func changelogEntries(upgrades []upgradeTask) []string {
	entries := make([]string, 0, len(upgrades))
	for _, up := range upgrades {
		label := "Terraform module"
		if up.kind == depKindImage {
			label = "container image"
		}
		entries = append(entries, fmt.Sprintf(
			"- changed the %s `%s` from `%s` to `%s`",
			label, extractRepoName(up.dep.Source), up.dep.CurrentVer, up.newVersion,
		))
	}
	return entries
}

// dependencyChanges states every upgrade as a dependency change for the
// changelog writer, which names them several to a line and merges a module the
// pending section already mentions. A module is named by the last segment of its
// source, as the per-upgrade statements always named it.
func dependencyChanges(upgrades []upgradeTask) []entities.DependencyChange {
	changes := make([]entities.DependencyChange, 0, len(upgrades))
	for _, up := range upgrades {
		subject := entities.SubjectTerraformModule
		if up.kind == depKindImage {
			subject = entities.SubjectContainerImage
		}
		changes = append(changes, entities.DependencyChange{
			Subject: subject,
			Name:    extractRepoName(up.dep.Source),
			From:    up.dep.CurrentVer,
			To:      up.newVersion,
		})
	}
	return changes
}

// generatePRDescription renders the pull request body: the upgrades, then the newer image
// tags it leaves out and why.
func generatePRDescription(tasks []upgradeTask, held []heldUpgrade) string {
	var sb strings.Builder
	sb.WriteString("## Summary\n\n")

	if len(tasks) <= maxDetailedUpgrades {
		sb.WriteString("This PR upgrades the following Terraform dependencies:\n\n")
		sb.WriteString("| Name | Type | Current Version | New Version | File |\n")
		sb.WriteString("|------|------|-----------------|-------------|------|\n")
		for _, t := range tasks {
			kindLabel := terraformBlockModule
			if t.kind == depKindImage {
				kindLabel = "image"
			}
			fmt.Fprintf(&sb, "| %s | %s | %s | %s | %s |\n",
				extractRepoName(t.dep.Source),
				kindLabel,
				t.dep.CurrentVer,
				t.newVersion,
				t.dep.FilePath)
		}
	} else {
		moduleCount, imageCount := countByKind(tasks)
		fmt.Fprintf(&sb, "This PR upgrades **%d** Terraform dependencies:\n\n",
			len(tasks))
		if moduleCount > 0 {
			fmt.Fprintf(&sb, "- **%d** module upgrades\n", moduleCount)
		}
		if imageCount > 0 {
			fmt.Fprintf(&sb, "- **%d** container image upgrades\n", imageCount)
		}
	}

	sb.WriteString(heldUpgradesSection(held))
	sb.WriteString("\n---\n")
	sb.WriteString("*This PR was automatically created by [autoupdate](https://github.com/rios0rios0/autoupdate)*\n")
	return sb.String()
}

// countByKind counts how many tasks belong to each depKind.
// Returns (moduleCount, imageCount).
func countByKind(tasks []upgradeTask) (int, int) {
	var moduleCount, imageCount int
	for _, t := range tasks {
		if t.kind == depKindImage {
			imageCount++
		} else {
			moduleCount++
		}
	}
	return moduleCount, imageCount
}
