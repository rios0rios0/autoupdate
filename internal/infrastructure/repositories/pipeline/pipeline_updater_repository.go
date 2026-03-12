package pipeline

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	logger "github.com/sirupsen/logrus"

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
	_ repositories.ProviderRepository,
	repo entities.Repository,
	_ entities.UpdateOptions,
) (*repositories.LocalUpdateResult, error) {
	logger.Infof("[pipeline] Scanning local clone of %s/%s for pipeline version references",
		repo.Organization, repo.Name)

	latestVersions := fetchAllLatestVersions(ctx)
	if len(latestVersions) == 0 {
		logger.Warnf("[pipeline] Could not fetch any latest versions, skipping")
		return nil, nil
	}

	upgrades, fileContents := localScanAndDetermineUpgrades(repoDir, latestVersions)
	if len(upgrades) == 0 {
		return nil, nil
	}

	logger.Infof("[pipeline] %s/%s: found %d version(s) to upgrade (local)",
		repo.Organization, repo.Name, len(upgrades))

	fileChanges := applyUpgrades(upgrades, fileContents)
	if err := support.WriteFileChanges(repoDir, fileChanges); err != nil {
		return nil, err
	}

	entries := make([]string, 0, len(upgrades))
	for _, up := range upgrades {
		entries = append(entries, fmt.Sprintf(
			"- changed the %s pipeline version from %s to %s",
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
	repoDir string,
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
	allFiles := append(yamlFiles, ymlFiles...)

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

		newMatch := strings.Replace(up.match.FullMatch, up.match.CurrentVer, up.newVersion, 1)
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
		return fmt.Sprintf(branchSingleFmt, tasks[0].match.Language, tasks[0].newVersion)
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
			"- changed the %s pipeline version from %s to %s",
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
