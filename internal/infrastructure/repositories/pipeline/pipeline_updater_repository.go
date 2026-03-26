package pipeline

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	logger "github.com/sirupsen/logrus"
	"golang.org/x/mod/semver"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/support"
	langPipeline "github.com/rios0rios0/langforge/pkg/infrastructure/languages/pipeline"
	langVersions "github.com/rios0rios0/langforge/pkg/infrastructure/versions"
)

const (
	updaterName         = "pipeline"
	maxDetailedUpgrades = 5
	branchSingleFmt     = "chore/upgrade-pipeline-%s-%s"
	branchBatchFmt      = "chore/upgrade-%d-pipeline-versions"
)

// ciSystem identifies the CI platform.
type ciSystem string

const (
	ciGitHubActions ciSystem = "github-actions"
	ciAzureDevOps   ciSystem = "azure-devops"
)

// languageRule holds scanning configuration for one language in one CI system.
type languageRule struct {
	Language string
	Patterns []*regexp.Regexp
}

// versionMatch represents a found version reference in a pipeline file.
type versionMatch struct {
	FilePath    string
	Language    string
	CurrentVer  string
	FullMatch   string // the entire matched substring for replacement
	Replacement string // the matched substring with the new version
}

// upgradeTask groups a version match with its target version.
type upgradeTask struct {
	match      versionMatch
	newVersion string
}

// refStyle describes the granularity of a GitHub Action version pin.
type refStyle int

const (
	refStyleMajor  refStyle = iota // @v4
	refStyleSemver                 // @v4.1.2
)

// actionRef represents a GitHub Action reference found in a workflow file.
type actionRef struct {
	FilePath   string
	Owner      string
	Repo       string
	CurrentRef string
	FullMatch  string
	RefStyle   refStyle
}

// actionUpgrade groups an action reference with its target version.
type actionUpgrade struct {
	ref    actionRef
	newRef string
}

// actionTagCache caches resolved tags per "owner/repo" key to avoid redundant API calls.
type actionTagCache map[string][]string

// actionUsesPattern matches GitHub Action references in workflow files.
// Captures: (1) the core uses clause (without trailing comment), (2) owner, (3) repo, (4) ref.
// Requires a v-prefix on the ref to skip SHA pins and branch refs.
// Allows optional quotes around the action string and an optional trailing inline comment.
var actionUsesPattern = regexp.MustCompile(
	`(?m)(uses:\s+['"]?([a-zA-Z0-9_.-]+)/([a-zA-Z0-9_.-]+)@(v\d+(?:\.\d+(?:\.\d+)?)?)['"]?)(?:\s+#.*)?$`,
)

// UpdaterRepository implements repositories.UpdaterRepository for CI/CD pipeline files.
type UpdaterRepository struct{}

// NewUpdaterRepository creates a new pipeline updater.
func NewUpdaterRepository() repositories.UpdaterRepository {
	return &UpdaterRepository{}
}

func (u *UpdaterRepository) Name() string { return updaterName }

// Detect returns true if the repository contains CI/CD pipeline configuration files.
func (u *UpdaterRepository) Detect(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) bool {
	found, err := support.DetectRemote(ctx, &langPipeline.Detector{}, provider, repo)
	if err != nil {
		logger.Warnf("[pipeline] detection error for %s/%s: %v", repo.Organization, repo.Name, err)
		return false
	}
	return found
}

// CreateUpdatePRs scans for outdated language version references in CI/CD
// pipeline files and creates a PR with the updates.
func (u *UpdaterRepository) CreateUpdatePRs(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
) ([]entities.PullRequest, error) {
	logger.Infof("[pipeline] Scanning %s/%s for pipeline version references", repo.Organization, repo.Name)

	latestVersions := fetchAllLatestVersions(ctx)
	if len(latestVersions) == 0 {
		logger.Warnf("[pipeline] Could not fetch any latest versions, skipping")
		return []entities.PullRequest{}, nil
	}

	upgrades, fileContents := scanAndDetermineUpgrades(ctx, provider, repo, latestVersions)
	if len(upgrades) == 0 {
		logger.Infof("[pipeline] %s/%s: all pipeline versions up to date", repo.Organization, repo.Name)
		return []entities.PullRequest{}, nil
	}

	logger.Infof("[pipeline] %s/%s: found %d version(s) to upgrade", repo.Organization, repo.Name, len(upgrades))

	if opts.DryRun {
		for _, up := range upgrades {
			logger.Infof(
				"[pipeline] [DRY RUN] Would upgrade %s: %s -> %s in %s",
				up.match.Language, up.match.CurrentVer, up.newVersion, up.match.FilePath,
			)
		}
		return []entities.PullRequest{}, nil
	}

	return createUpgradePR(ctx, provider, repo, opts, upgrades, fileContents)
}

// ApplyUpdates implements repositories.LocalUpdater for the clone-based pipeline.
// It scans the local filesystem for pipeline version references, fetches latest
// versions via HTTP, writes changes to disk, and returns PR metadata.
func (u *UpdaterRepository) ApplyUpdates(
	ctx context.Context,
	repoDir string,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	_ entities.UpdateOptions,
) (*repositories.LocalUpdateResult, error) {
	logger.Infof("[pipeline] Scanning local clone of %s/%s for pipeline version references",
		repo.Organization, repo.Name)

	latestVersions := fetchAllLatestVersions(ctx)
	if len(latestVersions) == 0 {
		logger.Warnf("[pipeline] Could not fetch any latest versions, skipping")
		return nil, repositories.ErrNoUpdatesNeeded
	}

	upgrades, fileContents := localScanAndDetermineUpgrades(ctx, repoDir, provider, latestVersions)
	if len(upgrades) == 0 {
		return nil, repositories.ErrNoUpdatesNeeded
	}

	logger.Infof("[pipeline] %s/%s: found %d version(s) to upgrade (local)",
		repo.Organization, repo.Name, len(upgrades))

	fileChanges := applyUpgrades(upgrades, fileContents)
	if len(fileChanges) == 0 {
		return nil, repositories.ErrNoUpdatesNeeded
	}
	if err := support.WriteFileChanges(repoDir, fileChanges); err != nil {
		return nil, err
	}

	entries := make([]string, 0, len(upgrades))
	for _, up := range upgrades {
		entries = append(entries, fmt.Sprintf(
			"- changed the %s pipeline version from `%s` to `%s`",
			up.match.Language, up.match.CurrentVer, up.newVersion,
		))
	}
	support.LocalChangelogUpdate(repoDir, entries)

	return &repositories.LocalUpdateResult{
		BranchName:    generateBranchName(upgrades),
		CommitMessage: generateCommitMessage(upgrades),
		PRTitle:       generatePRTitle(upgrades),
		PRDescription: generatePRDescription(upgrades),
	}, nil
}

// localScanAndDetermineUpgrades walks the local filesystem for YAML files,
// scans them for version references, and returns upgrade tasks plus file contents.
func localScanAndDetermineUpgrades(
	ctx context.Context,
	repoDir string,
	provider repositories.ProviderRepository,
	latestVersions map[string]string,
) ([]upgradeTask, map[string]string) {
	fileContents := make(map[string]string)
	var upgrades []upgradeTask

	yamlFiles, err := support.WalkFilesByExtension(repoDir, ".yaml")
	if err != nil {
		logger.Warnf("[pipeline] Failed to walk .yaml files: %v", err)
	}
	ymlFiles, ymlErr := support.WalkFilesByExtension(repoDir, ".yml")
	if ymlErr != nil {
		logger.Warnf("[pipeline] Failed to walk .yml files: %v", ymlErr)
	}
	allFiles := yamlFiles
	allFiles = append(allFiles, ymlFiles...)

	tagCache := make(actionTagCache)

	for _, relPath := range allFiles {
		ci := classifyFile(relPath)
		if ci == "" {
			continue
		}

		data, readErr := os.ReadFile(filepath.Join(repoDir, relPath))
		if readErr != nil {
			logger.Warnf("[pipeline] Failed to read %s: %v", relPath, readErr)
			continue
		}
		content := string(data)

		fileUpgrades := findUpgradesInFile(content, relPath, ci, latestVersions)

		if ci == ciGitHubActions && provider != nil {
			actionUpgrades := findActionUpgradesInFile(ctx, provider, content, relPath, tagCache)
			fileUpgrades = append(fileUpgrades, actionUpgrades...)
		}

		upgrades = append(upgrades, fileUpgrades...)

		if len(fileUpgrades) > 0 {
			fileContents[relPath] = content
		}
	}

	return upgrades, fileContents
}

// --- version fetching ---

// fetchAllLatestVersions fetches the latest version for every supported language.
func fetchAllLatestVersions(ctx context.Context) map[string]string {
	fetchers := languageFetchers()
	results := make(map[string]string, len(fetchers))

	for lang, fetcher := range fetchers {
		ver, err := fetcher(ctx)
		if err != nil {
			logger.Warnf("[pipeline] Failed to fetch latest %s version: %v", lang, err)
			continue
		}
		results[lang] = ver
		logger.Debugf("[pipeline] Latest %s version: %s", lang, ver)
	}

	return results
}

// languageFetchers returns the map of language name to version fetcher.
func languageFetchers() map[string]langVersions.VersionFetcher {
	return map[string]langVersions.VersionFetcher{
		"golang":    langVersions.FetchLatestGoVersion,
		"python":    langVersions.FetchLatestPythonVersion,
		"nodejs":    langVersions.FetchLatestNodeVersion,
		"java":      langVersions.FetchLatestJavaVersion,
		"terraform": langVersions.FetchLatestTerraformVersion,
	}
}

// --- scanning ---

// scanAndDetermineUpgrades lists pipeline files, scans them for version
// references, and returns the list of upgrades needed plus the file contents.
func scanAndDetermineUpgrades(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	latestVersions map[string]string,
) ([]upgradeTask, map[string]string) {
	fileContents := make(map[string]string)
	var upgrades []upgradeTask

	allFiles := listPipelineFiles(ctx, provider, repo)
	tagCache := make(actionTagCache)

	for _, f := range allFiles {
		if f.IsDir {
			continue
		}

		ci := classifyFile(f.Path)
		if ci == "" {
			continue
		}

		content, contentErr := provider.GetFileContent(ctx, repo, f.Path)
		if contentErr != nil {
			logger.Warnf("[pipeline] Failed to read %s: %v", f.Path, contentErr)
			continue
		}

		fileUpgrades := findUpgradesInFile(content, f.Path, ci, latestVersions)

		if ci == ciGitHubActions {
			actionUpgrades := findActionUpgradesInFile(ctx, provider, content, f.Path, tagCache)
			fileUpgrades = append(fileUpgrades, actionUpgrades...)
		}

		upgrades = append(upgrades, fileUpgrades...)

		if len(fileUpgrades) > 0 {
			fileContents[f.Path] = content
		}
	}

	return upgrades, fileContents
}

// listPipelineFiles collects all YAML files from the repository.
func listPipelineFiles(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) []entities.File {
	yamlFiles, err := provider.ListFiles(ctx, repo, ".yaml")
	if err != nil {
		logger.Warnf("[pipeline] Failed to list .yaml files: %v", err)
	}

	ymlFiles, ymlErr := provider.ListFiles(ctx, repo, ".yml")
	if ymlErr != nil {
		logger.Warnf("[pipeline] Failed to list .yml files: %v", ymlErr)
	}

	return append(yamlFiles, ymlFiles...)
}

// findUpgradesInFile scans a single file for version references and returns upgrade tasks.
func findUpgradesInFile(
	content, filePath string,
	ci ciSystem,
	latestVersions map[string]string,
) []upgradeTask {
	rules := rulesForCI(ci)
	matches := scanFileForVersions(content, filePath, rules)

	var tasks []upgradeTask
	for _, match := range matches {
		if !isExactVersion(match.CurrentVer) {
			continue
		}

		latestVer, ok := latestVersions[match.Language]
		if !ok {
			continue
		}

		truncated := truncateToGranularity(latestVer, match.CurrentVer)
		if truncated == match.CurrentVer {
			continue
		}

		tasks = append(tasks, upgradeTask{
			match:      match,
			newVersion: truncated,
		})
	}

	return tasks
}

// classifyFile determines which CI system a file belongs to based on its path.
func classifyFile(path string) ciSystem {
	if strings.HasPrefix(path, ".github/workflows/") {
		return ciGitHubActions
	}
	if strings.HasPrefix(path, "azure-devops/") ||
		path == ".azure-pipelines.yml" ||
		path == "azure-pipelines.yml" {
		return ciAzureDevOps
	}
	return ""
}

// scanFileForVersions applies all language rules and returns matches.
func scanFileForVersions(content, filePath string, rules []languageRule) []versionMatch {
	var matches []versionMatch

	for _, rule := range rules {
		for _, pattern := range rule.Patterns {
			found := pattern.FindAllStringSubmatch(content, -1)
			allIndices := pattern.FindAllStringIndex(content, -1)

			for i, m := range found {
				if len(m) < 2 { //nolint:mnd // need at least the full match and one capture group
					continue
				}

				version := m[len(m)-1] // last capture group is the version
				matches = append(matches, versionMatch{
					FilePath:   filePath,
					Language:   rule.Language,
					CurrentVer: version,
					FullMatch:  content[allIndices[i][0]:allIndices[i][1]],
				})
			}
		}
	}

	return matches
}

// --- GitHub Actions scanning ---

// scanFileForActions extracts GitHub Action references from a workflow file.
func scanFileForActions(content, filePath string) []actionRef {
	var refs []actionRef
	matches := actionUsesPattern.FindAllStringSubmatch(content, -1)

	for _, m := range matches {
		if len(m) < 5 { //nolint:mnd // need full match + 4 capture groups
			continue
		}
		refs = append(refs, actionRef{
			FilePath:   filePath,
			Owner:      m[2],
			Repo:       m[3],
			CurrentRef: m[4],
			FullMatch:  m[1],
			RefStyle:   classifyRefStyle(m[4]),
		})
	}

	return refs
}

// classifyRefStyle determines whether an action ref is major-only or full semver.
func classifyRefStyle(ref string) refStyle {
	parts := strings.Split(strings.TrimPrefix(ref, "v"), ".")
	if len(parts) == 1 {
		return refStyleMajor
	}
	return refStyleSemver
}

// --- CI system rules ---

// rulesForCI returns the scanning rules for a given CI system.
func rulesForCI(ci ciSystem) []languageRule {
	rules, ok := ciRules()[ci]
	if !ok {
		return nil
	}
	return rules
}

// ciRules returns all scanning rules organized by CI system.
func ciRules() map[ciSystem][]languageRule {
	return map[ciSystem][]languageRule{
		ciGitHubActions: githubActionsRules(),
		ciAzureDevOps:   azureDevOpsRules(),
	}
}

func githubActionsRules() []languageRule {
	return []languageRule{
		{
			Language: "golang",
			Patterns: []*regexp.Regexp{
				regexp.MustCompile(`go-version:\s*['"]([^'"]+)['"]`),
			},
		},
		{
			Language: "python",
			Patterns: []*regexp.Regexp{
				regexp.MustCompile(`python-version:\s*['"]([^'"]+)['"]`),
			},
		},
		{
			Language: "nodejs",
			Patterns: []*regexp.Regexp{
				regexp.MustCompile(`node-version:\s*['"]([^'"]+)['"]`),
			},
		},
		{
			Language: "java",
			Patterns: []*regexp.Regexp{
				regexp.MustCompile(`java-version:\s*['"]([^'"]+)['"]`),
			},
		},
	}
}

func azureDevOpsRules() []languageRule {
	return []languageRule{
		{
			Language: "golang",
			Patterns: []*regexp.Regexp{
				regexp.MustCompile(`(?s)GoTool@\d.*?version:\s*'([^']+)'`),
			},
		},
		{
			Language: "python",
			Patterns: []*regexp.Regexp{
				regexp.MustCompile(`(?s)UsePythonVersion@\d.*?versionSpec:\s*'([^']+)'`),
			},
		},
		{
			Language: "nodejs",
			Patterns: []*regexp.Regexp{
				regexp.MustCompile(`(?s)NodeTool@\d.*?version:\s*'([^']+)'`),
			},
		},
		{
			Language: "java",
			Patterns: []*regexp.Regexp{
				regexp.MustCompile(`(?s)JavaToolInstaller@\d.*?versionSpec:\s*'([^']+)'`),
			},
		},
		{
			Language: "terraform",
			Patterns: []*regexp.Regexp{
				regexp.MustCompile(`terraformVersion:\s*'([^']+)'`),
			},
		},
	}
}

// --- GitHub Actions tag resolution and version comparison ---

// resolveActionTags fetches tags for a GitHub Action repo, using the cache.
func resolveActionTags(
	ctx context.Context,
	provider repositories.ProviderRepository,
	owner, repo string,
	cache actionTagCache,
) []string {
	key := owner + "/" + repo
	if tags, ok := cache[key]; ok {
		return tags
	}

	actionRepo := entities.Repository{
		Organization: owner,
		Name:         repo,
	}
	tags, err := provider.GetTags(ctx, actionRepo)
	if err != nil {
		logger.Warnf("[pipeline] Failed to fetch tags for %s/%s: %v", owner, repo, err)
		cache[key] = nil
		return nil
	}

	cache[key] = tags
	return tags
}

// determineActionUpgrade compares the current action ref against available tags
// and returns an upgrade if one is available.
func determineActionUpgrade(ref actionRef, tags []string) *actionUpgrade {
	if len(tags) == 0 {
		return nil
	}

	switch ref.RefStyle {
	case refStyleMajor:
		return findMajorUpgrade(ref, tags)
	case refStyleSemver:
		return findSemverUpgrade(ref, tags)
	default:
		return nil
	}
}

// findMajorUpgrade checks if a higher major version exists.
func findMajorUpgrade(ref actionRef, tags []string) *actionUpgrade {
	currentMajor := extractMajor(ref.CurrentRef)
	if currentMajor < 0 {
		return nil
	}

	latestMajor := -1
	for _, tag := range tags {
		m := extractMajor(tag)
		if m > latestMajor {
			latestMajor = m
		}
	}

	if latestMajor > currentMajor {
		return &actionUpgrade{
			ref:    ref,
			newRef: fmt.Sprintf("v%d", latestMajor),
		}
	}
	return nil
}

// findSemverUpgrade finds the latest tag within the same major version.
func findSemverUpgrade(ref actionRef, tags []string) *actionUpgrade {
	currentMajor := extractMajor(ref.CurrentRef)
	if currentMajor < 0 {
		return nil
	}

	currentNorm := normalizeActionVersion(ref.CurrentRef)
	var bestTag string

	for _, tag := range tags {
		norm := normalizeActionVersion(tag)
		if !semver.IsValid(norm) {
			continue
		}
		if extractMajor(tag) != currentMajor {
			continue
		}
		if semver.Compare(norm, currentNorm) > 0 {
			if bestTag == "" || semver.Compare(norm, normalizeActionVersion(bestTag)) > 0 {
				bestTag = tag
			}
		}
	}

	if bestTag != "" {
		return &actionUpgrade{
			ref:    ref,
			newRef: bestTag,
		}
	}
	return nil
}

// extractMajor parses the major version number from a ref like "v4" or "v4.1.2".
func extractMajor(ref string) int {
	s := strings.TrimPrefix(ref, "v")
	parts := strings.SplitN(s, ".", 2) //nolint:mnd // split into major + rest
	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return -1
	}
	return n
}

// normalizeActionVersion ensures the version has a "v" prefix and expands to 3-part for semver.
func normalizeActionVersion(ref string) string {
	if !strings.HasPrefix(ref, "v") {
		ref = "v" + ref
	}
	parts := strings.Split(strings.TrimPrefix(ref, "v"), ".")
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	return "v" + strings.Join(parts, ".")
}

// findActionUpgradesInFile scans a workflow file for GitHub Action references
// and returns upgrade tasks using the existing upgradeTask type.
func findActionUpgradesInFile(
	ctx context.Context,
	provider repositories.ProviderRepository,
	content, filePath string,
	cache actionTagCache,
) []upgradeTask {
	refs := scanFileForActions(content, filePath)
	var tasks []upgradeTask

	for _, ref := range refs {
		tags := resolveActionTags(ctx, provider, ref.Owner, ref.Repo, cache)
		up := determineActionUpgrade(ref, tags)
		if up == nil {
			continue
		}

		tasks = append(tasks, upgradeTask{
			match: versionMatch{
				FilePath:   filePath,
				Language:   fmt.Sprintf("action:%s/%s", ref.Owner, ref.Repo),
				CurrentVer: ref.CurrentRef,
				FullMatch:  ref.FullMatch,
			},
			newVersion: up.newRef,
		})
	}

	return tasks
}

// --- version validation ---

// versionPattern matches simple dotted numeric versions like "1.25", "1.25.7", "21".
var versionPattern = regexp.MustCompile(`^\d+(\.\d+)*$`)

// isExactVersion returns true if ver is a simple dotted numeric version
// (e.g. "1.25", "1.25.7", "21"). It rejects ranges, wildcards, and
// constraint operators like "1.20.x", "3.x", ">=1.20", "~1.20".
func isExactVersion(ver string) bool {
	return versionPattern.MatchString(ver)
}

// --- version granularity ---

// truncateToGranularity truncates the latest version to match the number
// of parts in the reference version. For example, if the reference is
// "1.25" (2 parts) and latest is "1.26.0", this returns "1.26".
func truncateToGranularity(latest, reference string) string {
	refParts := strings.Split(reference, ".")
	latestParts := strings.Split(latest, ".")

	if len(refParts) >= len(latestParts) {
		return latest
	}

	return strings.Join(latestParts[:len(refParts)], ".")
}

// --- string helpers ---

// sanitizeBranchSegment replaces characters illegal in Git branch names.
func sanitizeBranchSegment(s string) string {
	r := strings.NewReplacer(":", "-", "/", "-")
	return r.Replace(s)
}

// replaceLastOccurrence replaces only the last occurrence of old in s with replacement.
// This is used instead of [strings.Replace] to ensure multi-line regex matches
// replace the version in the actual config key (e.g. versionSpec) rather than
// in a preceding label (e.g. displayName).
func replaceLastOccurrence(s, old, replacement string) string {
	idx := strings.LastIndex(s, old)
	if idx < 0 {
		return s
	}
	return s[:idx] + replacement + s[idx+len(old):]
}

// stripDisplayNameVersion removes the version number from displayName lines
// within a matched task block. For example, "displayName: '🐍 Use Python 3.12'"
// becomes "displayName: '🐍 Use Python'".
func stripDisplayNameVersion(s, version string) string {
	pattern := regexp.MustCompile(
		`(displayName:\s*['"][^'"]*?)\s+` + regexp.QuoteMeta(version) + `([^'"]*['"])`,
	)
	return pattern.ReplaceAllString(s, "${1}${2}")
}

// --- upgrade application ---

// applyUpgrades applies version replacements to file contents.
func applyUpgrades(upgrades []upgradeTask, fileContents map[string]string) []entities.FileChange {
	modified := make(map[string]string, len(fileContents))
	maps.Copy(modified, fileContents)

	for _, up := range upgrades {
		content, ok := modified[up.match.FilePath]
		if !ok {
			continue
		}

		newMatch := replaceLastOccurrence(up.match.FullMatch, up.match.CurrentVer, up.newVersion)
		newMatch = stripDisplayNameVersion(newMatch, up.match.CurrentVer)
		content = strings.Replace(content, up.match.FullMatch, newMatch, 1)
		modified[up.match.FilePath] = content
	}

	var changes []entities.FileChange
	for path, content := range modified {
		if content != fileContents[path] {
			changes = append(changes, entities.FileChange{
				Path:       path,
				Content:    content,
				ChangeType: "edit",
			})
		}
	}

	return changes
}

// --- PR creation ---

func createUpgradePR(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
	upgrades []upgradeTask,
	fileContents map[string]string,
) ([]entities.PullRequest, error) {
	branchName := generateBranchName(upgrades)

	exists, prCheckErr := provider.PullRequestExists(ctx, repo, branchName)
	if prCheckErr != nil {
		logger.Warnf("[pipeline] Failed to check existing PRs: %v", prCheckErr)
	}
	if exists {
		logger.Infof("[pipeline] PR already exists for branch %q, skipping", branchName)
		return []entities.PullRequest{}, nil
	}

	fileChanges := applyUpgrades(upgrades, fileContents)
	fileChanges = appendChangelogEntry(ctx, provider, repo, upgrades, fileChanges)

	targetBranch := repo.DefaultBranch
	if opts.TargetBranch != "" {
		targetBranch = "refs/heads/" + opts.TargetBranch
	}

	err := provider.CreateBranchWithChanges(ctx, repo, entities.BranchInput{
		BranchName:    branchName,
		BaseBranch:    targetBranch,
		Changes:       fileChanges,
		CommitMessage: generateCommitMessage(upgrades),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create branch: %w", err)
	}

	pr, createErr := provider.CreatePullRequest(ctx, repo, entities.PullRequestInput{
		SourceBranch: "refs/heads/" + branchName,
		TargetBranch: targetBranch,
		Title:        generatePRTitle(upgrades),
		Description:  generatePRDescription(upgrades),
		AutoComplete: opts.AutoComplete,
	})
	if createErr != nil {
		return nil, fmt.Errorf("failed to create PR: %w", createErr)
	}

	logger.Infof("[pipeline] Created PR #%d for %s/%s: %s", pr.ID, repo.Organization, repo.Name, pr.URL)
	return []entities.PullRequest{*pr}, nil
}

// --- PR text generation ---

func generateBranchName(tasks []upgradeTask) string {
	if len(tasks) == 1 {
		return fmt.Sprintf(
			branchSingleFmt,
			sanitizeBranchSegment(tasks[0].match.Language),
			tasks[0].newVersion,
		)
	}
	return fmt.Sprintf(branchBatchFmt, len(tasks))
}

func generateCommitMessage(tasks []upgradeTask) string {
	if len(tasks) == 1 {
		return fmt.Sprintf(
			"chore(deps): upgraded %s pipeline version from `%s` to `%s`",
			tasks[0].match.Language, tasks[0].match.CurrentVer, tasks[0].newVersion,
		)
	}
	return fmt.Sprintf("chore(deps): upgraded %d pipeline version references", len(tasks))
}

func generatePRTitle(tasks []upgradeTask) string {
	if len(tasks) == 1 {
		return fmt.Sprintf(
			"chore(deps): upgraded %s pipeline version to `%s`",
			tasks[0].match.Language, tasks[0].newVersion,
		)
	}
	return fmt.Sprintf("chore(deps): upgraded %d pipeline version references", len(tasks))
}

func generatePRDescription(tasks []upgradeTask) string {
	var sb strings.Builder
	sb.WriteString("## Summary\n\n")

	if len(tasks) <= maxDetailedUpgrades {
		sb.WriteString("This PR upgrades the following CI/CD pipeline version references:\n\n")
		sb.WriteString("| Language | Current Version | New Version | File |\n")
		sb.WriteString("|----------|-----------------|-------------|------|\n")
		for _, t := range tasks {
			fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n",
				t.match.Language, t.match.CurrentVer, t.newVersion, t.match.FilePath)
		}
	} else {
		fmt.Fprintf(&sb, "This PR upgrades **%d** pipeline version references.\n", len(tasks))
	}

	sb.WriteString("\n---\n")
	sb.WriteString("*This PR was automatically created by [autoupdate](https://github.com/rios0rios0/autoupdate)*\n")
	return sb.String()
}

// appendChangelogEntry reads CHANGELOG.md (if present), inserts entries
// describing the pipeline version upgrades, and appends the modified file
// to the change set.
func appendChangelogEntry(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	upgrades []upgradeTask,
	fileChanges []entities.FileChange,
) []entities.FileChange {
	if !provider.HasFile(ctx, repo, "CHANGELOG.md") {
		return fileChanges
	}

	content, err := provider.GetFileContent(ctx, repo, "CHANGELOG.md")
	if err != nil {
		logger.Warnf("[pipeline] Failed to read CHANGELOG.md: %v", err)
		return fileChanges
	}

	entries := make([]string, 0, len(upgrades))
	for _, up := range upgrades {
		entries = append(entries, fmt.Sprintf(
			"- changed the %s pipeline version from `%s` to `%s`",
			up.match.Language, up.match.CurrentVer, up.newVersion,
		))
	}

	modified := entities.InsertChangelogEntry(content, entries)
	if modified == content {
		return fileChanges
	}

	return append(fileChanges, entities.FileChange{
		Path:       "CHANGELOG.md",
		Content:    modified,
		ChangeType: "edit",
	})
}
