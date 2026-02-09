package terraform

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	logger "github.com/sirupsen/logrus"
	"github.com/zclconf/go-cty/cty"
	"golang.org/x/mod/semver"

	"github.com/rios0rios0/autoupdate/domain"
)

const (
	updaterName     = "terraform"
	minMatchLen     = 6
	branchBatchFmt  = "chore/upgrade-%d-modules"
	branchSingleFmt = "chore/upgrade-%s-%s"
)

// Updater implements domain.Updater for Terraform module dependencies.
// It reads files via the provider API, detects version refs, and creates PRs
// with updated version strings — no local clone required.
type Updater struct{}

// New creates a new Terraform updater.
func New() domain.Updater {
	return &Updater{}
}

func (u *Updater) Name() string { return updaterName }

// Detect returns true if the repository contains .tf files.
func (u *Updater) Detect(
	ctx context.Context,
	provider domain.Provider,
	repo domain.Repository,
) bool {
	files, err := provider.ListFiles(ctx, repo, ".tf")
	if err != nil {
		return false
	}
	return len(files) > 0
}

// CreateUpdatePRs scans for outdated Terraform module dependencies,
// groups upgrades by repository, and creates PRs with the changes.
func (u *Updater) CreateUpdatePRs(
	ctx context.Context,
	provider domain.Provider,
	repo domain.Repository,
	opts domain.UpdateOptions,
) ([]domain.PullRequest, error) {
	logger.Infof(
		"[terraform] Scanning %s/%s for Terraform dependencies",
		repo.Organization, repo.Name,
	)

	allDeps, err := u.scanAllDependencies(ctx, provider, repo)
	if err != nil {
		return nil, err
	}
	if len(allDeps) == 0 {
		return []domain.PullRequest{}, nil
	}

	upgrades := u.determineUpgrades(ctx, provider, repo, allDeps)
	if len(upgrades) == 0 {
		logger.Infof(
			"[terraform] %s/%s: all Terraform dependencies up to date",
			repo.Organization, repo.Name,
		)
		return []domain.PullRequest{}, nil
	}

	logger.Infof(
		"[terraform] %s/%s: found %d dependencies to upgrade",
		repo.Organization, repo.Name, len(upgrades),
	)

	if opts.DryRun {
		for _, up := range upgrades {
			logger.Infof(
				"[terraform] [DRY RUN] Would upgrade %s: %s -> %s",
				extractRepoName(up.dep.Source), up.dep.CurrentVer, up.newVersion,
			)
		}
		return []domain.PullRequest{}, nil
	}

	return u.createUpgradePR(ctx, provider, repo, opts, upgrades)
}

// scanAllDependencies lists .tf files and parses them for module deps.
func (u *Updater) scanAllDependencies(
	ctx context.Context,
	provider domain.Provider,
	repo domain.Repository,
) ([]depWithContent, error) {
	tfFiles, err := provider.ListFiles(ctx, repo, ".tf")
	if err != nil {
		return nil, fmt.Errorf("failed to list .tf files: %w", err)
	}

	var allDeps []depWithContent
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
			})
		}
	}

	return allDeps, nil
}

// determineUpgrades resolves tags and determines which deps need upgrading.
func (u *Updater) determineUpgrades(
	ctx context.Context,
	provider domain.Provider,
	repo domain.Repository,
	allDeps []depWithContent,
) []upgradeTask {
	moduleVersions := make(map[string][]string)
	for _, dc := range allDeps {
		src := dc.Dependency.Source
		if _, ok := moduleVersions[src]; ok {
			continue
		}
		tags := resolveTagsForSource(ctx, provider, repo, src)
		moduleVersions[src] = tags
	}

	var upgrades []upgradeTask
	for _, dc := range allDeps {
		tags := moduleVersions[dc.Dependency.Source]
		if len(tags) == 0 {
			continue
		}
		latestVersion := tags[0]
		if dc.Dependency.CurrentVer == latestVersion {
			continue
		}
		if !isNewerVersion(dc.Dependency.CurrentVer, latestVersion) {
			continue
		}
		upgrades = append(upgrades, upgradeTask{
			dep:         dc.Dependency,
			newVersion:  latestVersion,
			fileContent: dc.FileContent,
		})
	}

	return upgrades
}

// createUpgradePR creates a branch with changes and opens a PR.
func (u *Updater) createUpgradePR(
	ctx context.Context,
	provider domain.Provider,
	repo domain.Repository,
	opts domain.UpdateOptions,
	upgrades []upgradeTask,
) ([]domain.PullRequest, error) {
	branchName := generateBranchName(upgrades)

	// Check if PR already exists
	exists, prCheckErr := provider.PullRequestExists(ctx, repo, branchName)
	if prCheckErr != nil {
		logger.Warnf("[terraform] Failed to check existing PRs: %v", prCheckErr)
	}
	if exists {
		logger.Infof(
			"[terraform] PR already exists for branch %q, skipping",
			branchName,
		)
		return []domain.PullRequest{}, nil
	}

	fileChanges := applyUpgrades(upgrades)
	targetBranch := repo.DefaultBranch
	if opts.TargetBranch != "" {
		targetBranch = "refs/heads/" + opts.TargetBranch
	}

	err := provider.CreateBranchWithChanges(ctx, repo, domain.BranchInput{
		BranchName:    branchName,
		BaseBranch:    targetBranch,
		Changes:       fileChanges,
		CommitMessage: generateCommitMessage(upgrades),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create branch: %w", err)
	}

	pr, createErr := provider.CreatePullRequest(ctx, repo, domain.PullRequestInput{
		SourceBranch: "refs/heads/" + branchName,
		TargetBranch: targetBranch,
		Title:        generatePRTitle(upgrades),
		Description:  generatePRDescription(upgrades),
		AutoComplete: opts.AutoComplete,
	})
	if createErr != nil {
		return nil, fmt.Errorf("failed to create PR: %w", createErr)
	}

	logger.Infof(
		"[terraform] Created PR #%d for %s/%s: %s",
		pr.ID, repo.Organization, repo.Name, pr.URL,
	)
	return []domain.PullRequest{*pr}, nil
}

// --- internal types ---

type depWithContent struct {
	Dependency  domain.Dependency
	FileContent string
}

type upgradeTask struct {
	dep         domain.Dependency
	newVersion  string
	fileContent string
}

// --- scanning ---

func scanTerraformFile(content, filePath string) []domain.Dependency {
	parser := hclparse.NewParser()

	file, diags := parser.ParseHCL([]byte(content), filePath)
	if diags.HasErrors() {
		return scanWithRegex(content, filePath)
	}

	body := file.Body
	if body == nil {
		return nil
	}

	bodyContent, _, partialDiags := body.PartialContent(&hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "module", LabelNames: []string{"name"}},
		},
	})
	if partialDiags.HasErrors() {
		return scanWithRegex(content, filePath)
	}

	var deps []domain.Dependency
	for _, block := range bodyContent.Blocks {
		if block.Type != "module" {
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
		deps = append(deps, domain.Dependency{
			Name:       moduleName,
			Source:     cleanSource,
			CurrentVer: version,
			FilePath:   filePath,
			Line:       block.DefRange.Start.Line,
		})
	}

	return deps
}

func scanWithRegex(content, filePath string) []domain.Dependency {
	var deps []domain.Dependency

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

		deps = append(deps, domain.Dependency{
			Name:       moduleName,
			Source:     cleanSource,
			CurrentVer: version,
			FilePath:   filePath,
			Line:       lineNum,
		})
	}

	return deps
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

func isNewerVersion(current, newVersion string) bool {
	cur := normalizeVersion(current)
	nv := normalizeVersion(newVersion)
	if semver.IsValid(cur) && semver.IsValid(nv) {
		return semver.Compare(nv, cur) > 0
	}
	return newVersion > current
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}

// --- upgrade application ---

func applyUpgrades(tasks []upgradeTask) []domain.FileChange {
	// Group by file path
	fileContent := make(map[string]string)
	for _, t := range tasks {
		if _, ok := fileContent[t.dep.FilePath]; !ok {
			fileContent[t.dep.FilePath] = t.fileContent
		}
	}

	// Apply each upgrade to the file content
	for _, t := range tasks {
		content := fileContent[t.dep.FilePath]
		content = applyVersionUpgrade(content, t.dep, t.newVersion)
		fileContent[t.dep.FilePath] = content
	}

	var changes []domain.FileChange
	for path, content := range fileContent {
		changes = append(changes, domain.FileChange{
			Path:       path,
			Content:    content,
			ChangeType: "edit",
		})
	}
	return changes
}

func applyVersionUpgrade(
	content string,
	dep domain.Dependency,
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

func resolveTagsForSource(
	ctx context.Context,
	provider domain.Provider,
	currentRepo domain.Repository,
	source string,
) []string {
	repoName := extractRepoName(source)
	if repoName == "" {
		return nil
	}

	allRepos, err := provider.DiscoverRepositories(
		ctx, currentRepo.Organization,
	)
	if err != nil {
		return nil
	}

	for _, r := range allRepos {
		if r.Name == repoName {
			tags, tagsErr := provider.GetTags(ctx, r)
			if tagsErr != nil {
				return nil
			}
			return tags
		}
	}

	return nil
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
			"chore(deps): upgrade %s from %s to %s",
			extractRepoName(tasks[0].dep.Source),
			tasks[0].dep.CurrentVer,
			tasks[0].newVersion,
		)
	}
	return fmt.Sprintf(
		"chore(deps): upgrade %d Terraform module dependencies",
		len(tasks),
	)
}

func generatePRTitle(tasks []upgradeTask) string {
	if len(tasks) == 1 {
		return fmt.Sprintf(
			"chore(deps): upgrade %s to %s",
			extractRepoName(tasks[0].dep.Source),
			tasks[0].newVersion,
		)
	}
	return fmt.Sprintf(
		"chore(deps): upgrade %d Terraform module dependencies",
		len(tasks),
	)
}

func generatePRDescription(tasks []upgradeTask) string {
	var sb strings.Builder
	sb.WriteString("## Summary\n\n")
	sb.WriteString("This PR upgrades the following Terraform module dependencies:\n\n")
	sb.WriteString("| Module | Current Version | New Version | File |\n")
	sb.WriteString("|--------|-----------------|-------------|------|\n")
	for _, t := range tasks {
		sb.WriteString(fmt.Sprintf(
			"| %s | %s | %s | %s |\n",
			extractRepoName(t.dep.Source),
			t.dep.CurrentVer,
			t.newVersion,
			t.dep.FilePath,
		))
	}
	sb.WriteString("\n---\n")
	sb.WriteString("*This PR was automatically created by [autoupdate](https://github.com/rios0rios0/autoupdate)*\n")
	return sb.String()
}
