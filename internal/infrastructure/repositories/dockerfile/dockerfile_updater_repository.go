package dockerfile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/support"
	langDockerfile "github.com/rios0rios0/langforge/pkg/infrastructure/languages/dockerfile"
)

const (
	updaterName         = "dockerfile"
	maxDetailedUpgrades = 5
	branchSingleFmt     = "chore/upgrade-%s-%s"
	branchBatchFmt      = "chore/upgrade-%d-docker-images"
)

// fromPattern matches Docker FROM clauses with pinned version tags.
// Groups: (1) image name, (2) tag, (3) the digest pinned alongside the tag, if
// the clause carries one.
//
// The digest is captured rather than ignored because it is what Docker actually
// resolves: a clause reading "python:3.13-slim@sha256:..." pulls the manifest
// the digest names, whatever the tag beside it says. Matching it is what lets
// the rewrite move both halves together.
var fromPattern = regexp.MustCompile(
	`(?m)^FROM\s+` +
		`(?:--platform=[^\s]+\s+)?` +
		`([a-zA-Z0-9][a-zA-Z0-9._/-]*)` +
		`:` +
		`([a-zA-Z0-9][a-zA-Z0-9._-]*)` +
		`(?:@([a-zA-Z0-9]+:[a-fA-F0-9]{32,}))?` +
		`(?:\s+[Aa][Ss]\s+\S+)?`,
)

// imageRef holds a parsed FROM clause with its file context.
type imageRef struct {
	dep         entities.Dependency
	parsed      *parsedImageRef
	fileContent string
}

// upgradeTask groups an image reference with its target tag.
type upgradeTask struct {
	dep    entities.Dependency
	newTag string
	parsed *parsedImageRef

	// newDigest is the manifest newTag resolves to, carried only for a clause
	// that pins one. It comes from the same registry listing that produced
	// newTag, so the pair written back is the pair the registry reports.
	newDigest string
}

// UpdaterRepository implements repositories.UpdaterRepository for Dockerfile base images.
type UpdaterRepository struct{}

// NewUpdaterRepository creates a new Dockerfile updater.
func NewUpdaterRepository() repositories.UpdaterRepository {
	return &UpdaterRepository{}
}

func (u *UpdaterRepository) Name() string { return updaterName }

// Detect returns true if the repository contains Dockerfiles.
func (u *UpdaterRepository) Detect(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) bool {
	found, err := support.DetectRemote(ctx, &langDockerfile.Detector{}, provider, repo)
	if err != nil {
		logger.Warnf("[dockerfile] detection error for %s/%s: %v", repo.Organization, repo.Name, err)
		return false
	}
	return found
}

// CreateUpdatePRs scans Dockerfiles for outdated base image versions,
// resolves latest tags from Docker Hub, and creates a PR with updates.
func (u *UpdaterRepository) CreateUpdatePRs(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
) ([]entities.PullRequest, error) {
	logger.Infof("[dockerfile] Scanning %s/%s for Dockerfile base images", repo.Organization, repo.Name)

	allRefs := scanAllDockerfiles(ctx, provider, repo)
	if len(allRefs) == 0 {
		return []entities.PullRequest{}, nil
	}

	upgrades := determineUpgrades(ctx, allRefs)
	if len(upgrades) == 0 {
		logger.Infof("[dockerfile] %s/%s: all Dockerfile base images up to date", repo.Organization, repo.Name)
		return []entities.PullRequest{}, nil
	}

	logger.Infof("[dockerfile] %s/%s: found %d image(s) to upgrade", repo.Organization, repo.Name, len(upgrades))

	if opts.DryRun {
		for _, up := range upgrades {
			logger.Infof(
				"[dockerfile] [DRY RUN] Would upgrade %s: %s -> %s in %s",
				up.parsed.FullName(), up.dep.CurrentVer, up.newTag, up.dep.FilePath,
			)
		}
		return []entities.PullRequest{}, nil
	}

	return createUpgradePR(ctx, provider, repo, opts, upgrades, allRefs)
}

// ApplyUpdates implements repositories.LocalUpdater for the clone-based pipeline.
// It scans the local filesystem for Dockerfiles, fetches latest tags from Docker Hub,
// writes changes to disk, and returns PR metadata.
func (u *UpdaterRepository) ApplyUpdates(
	ctx context.Context,
	repoDir string,
	_ repositories.ProviderRepository,
	repo entities.Repository,
	_ entities.UpdateOptions,
) (*repositories.LocalUpdateResult, error) {
	logger.Infof("[dockerfile] Scanning local clone of %s/%s for Dockerfile base images",
		repo.Organization, repo.Name)

	allRefs := localScanAllDockerfiles(repoDir)
	if len(allRefs) == 0 {
		return nil, repositories.ErrNoUpdatesNeeded
	}

	upgrades := determineUpgrades(ctx, allRefs)
	if len(upgrades) == 0 {
		return nil, repositories.ErrNoUpdatesNeeded
	}

	logger.Infof("[dockerfile] %s/%s: found %d image(s) to upgrade (local)",
		repo.Organization, repo.Name, len(upgrades))

	fileChanges := applyUpgrades(upgrades, allRefs)
	if err := support.WriteFileChanges(repoDir, fileChanges); err != nil {
		return nil, err
	}

	support.LocalChangelogUpdate(repoDir, changelogEntries(upgrades))

	return &repositories.LocalUpdateResult{
		BranchName:    generateBranchName(upgrades),
		CommitMessage: generateCommitMessage(upgrades),
		PRTitle:       generatePRTitle(upgrades),
		PRDescription: generatePRDescription(upgrades),
	}, nil
}

// localScanAllDockerfiles walks the local filesystem for Dockerfiles
// and parses them for base image references.
func localScanAllDockerfiles(repoDir string) []imageRef {
	var allRefs []imageRef

	files, err := support.WalkFilesByPredicate(repoDir, isDockerfilePath)
	if err != nil {
		logger.Warnf("[dockerfile] Failed to walk Dockerfile files: %v", err)
		return nil
	}

	for _, relPath := range files {
		data, readErr := os.ReadFile(filepath.Join(repoDir, relPath))
		if readErr != nil {
			logger.Warnf("[dockerfile] Failed to read %s: %v", relPath, readErr)
			continue
		}

		refs := scanDockerfile(string(data), relPath)
		allRefs = append(allRefs, refs...)
	}

	return allRefs
}

// --- scanning ---

func scanAllDockerfiles(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) []imageRef {
	var allRefs []imageRef

	// List files that might be Dockerfiles. The provider's ListFiles accepts
	// an extension/pattern. We search for "Dockerfile" as a name pattern.
	files, err := provider.ListFiles(ctx, repo, "Dockerfile")
	if err != nil {
		logger.Warnf("[dockerfile] Failed to list Dockerfile files: %v", err)
		return nil
	}

	for _, f := range files {
		if f.IsDir || !isDockerfilePath(f.Path) {
			continue
		}

		content, contentErr := provider.GetFileContent(ctx, repo, f.Path)
		if contentErr != nil {
			logger.Warnf("[dockerfile] Failed to read %s: %v", f.Path, contentErr)
			continue
		}

		refs := scanDockerfile(content, f.Path)
		allRefs = append(allRefs, refs...)
	}

	return allRefs
}

// isDockerfilePath returns true if the file path looks like a Dockerfile.
func isDockerfilePath(path string) bool {
	base := path
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		base = path[idx+1:]
	}

	return base == "Dockerfile" ||
		strings.HasPrefix(base, "Dockerfile.") ||
		strings.HasSuffix(base, ".Dockerfile")
}

// isDockerHubImage returns true if the image name refers to a Docker Hub image.
// Registry-qualified images (e.g., "ghcr.io/org/image", "quay.io/image",
// "registry.example.com:5000/image") have a first path segment containing a dot
// or colon, which official Docker Hub images never have.
func isDockerHubImage(imageName string) bool {
	if !strings.Contains(imageName, "/") {
		return true // official image like "golang", "python"
	}
	firstSegment, _, _ := strings.Cut(imageName, "/")
	return !strings.ContainsAny(firstSegment, ".:")
}

// scanDockerfile parses FROM clauses in a Dockerfile and returns image references.
func scanDockerfile(content, filePath string) []imageRef {
	var refs []imageRef

	matches := fromPattern.FindAllStringSubmatch(content, -1)
	matchIndices := fromPattern.FindAllStringIndex(content, -1)

	for i, m := range matches {
		imageName := m[1]
		tag := m[2]
		digest := m[3]

		// Skip build args and scratch
		if strings.HasPrefix(imageName, "$") || strings.HasPrefix(imageName, "${") ||
			imageName == "scratch" {
			continue
		}

		// Skip non-Docker Hub registry images (e.g., ghcr.io/org/image, quay.io/image)
		if !isDockerHubImage(imageName) {
			continue
		}

		// Skip non-version tags
		if tag == "latest" || tag == "edge" || tag == "stable" {
			continue
		}

		version, suffix, precision, ok := parseTag(tag)
		if !ok {
			continue
		}

		// Parse namespace/image
		namespace := ""
		image := imageName
		if strings.Contains(imageName, "/") {
			parts := strings.SplitN(imageName, "/", 2) //nolint:mnd // split into namespace/image
			namespace = parts[0]
			image = parts[1]
		}

		lineNum := strings.Count(content[:matchIndices[i][0]], "\n") + 1

		refs = append(refs, imageRef{
			dep: entities.Dependency{
				Name:       imageName,
				Source:     imageName,
				CurrentVer: tag,
				FilePath:   filePath,
				Line:       lineNum,
			},
			parsed: &parsedImageRef{
				Namespace: namespace,
				Image:     image,
				Tag:       tag,
				Version:   version,
				Suffix:    suffix,
				Precision: precision,
				Digest:    digest,
			},
			fileContent: content,
		})
	}

	return refs
}

// --- upgrade determination ---

// fetchTagsFunc is the function used to fetch tags from the registry.
// It defaults to fetchTags and can be overridden in tests.
var fetchTagsFunc = fetchTags //nolint:gochecknoglobals // test override for DI

func determineUpgrades(ctx context.Context, allRefs []imageRef) []upgradeTask {
	// Cache tags per image to avoid redundant API calls
	tagCache := make(map[string]*tagIndex)
	var upgrades []upgradeTask

	for _, ref := range allRefs {
		cacheKey := ref.parsed.FullName()
		tags, cached := tagCache[cacheKey]
		if !cached {
			fetched, err := fetchTagsFunc(ctx, ref.parsed)
			if err != nil {
				logger.Warnf("[dockerfile] Failed to fetch tags for %s: %v", cacheKey, err)
				tagCache[cacheKey] = nil
				continue
			}
			tags = newTagIndex(fetched)
			tagCache[cacheKey] = tags
		}

		if tags == nil {
			continue
		}

		bestTag := findBestUpgrade(ref.parsed, tags.names)
		if bestTag == "" || bestTag == ref.parsed.Tag {
			continue
		}

		task, ok := newUpgradeTask(ref, bestTag, tags)
		if !ok {
			continue
		}
		upgrades = append(upgrades, task)
	}

	return upgrades
}

// tagIndex holds one image's published tags: the names the upgrade search runs
// over, and the manifest each name resolves to. Both come out of the same
// listing, which is what makes re-pinning a digest free.
type tagIndex struct {
	names   []string
	digests map[string]string
}

// newTagIndex splits a registry listing into the two shapes the upgrade needs.
func newTagIndex(tags []registryTag) *tagIndex {
	index := &tagIndex{
		names:   make([]string, 0, len(tags)),
		digests: make(map[string]string, len(tags)),
	}

	for _, tag := range tags {
		index.names = append(index.names, tag.Name)
		if tag.Digest != "" {
			index.digests[tag.Name] = tag.Digest
		}
	}

	return index
}

// newUpgradeTask pairs a reference with the tag it moves to, resolving the
// digest to write beside it when the clause pins one.
//
// A digest-pinned clause the registry reports no digest for is left alone
// rather than upgraded. The two alternatives are both worse: writing the tag on
// its own leaves the previous manifest pinned next to a version it is not --
// a pull request that reads as an upgrade and changes nothing that gets built --
// and dropping the digest silently un-pins a base image the repository pinned
// on purpose.
func newUpgradeTask(ref imageRef, newTag string, tags *tagIndex) (upgradeTask, bool) {
	task := upgradeTask{dep: ref.dep, newTag: newTag, parsed: ref.parsed}
	if ref.parsed.Digest == "" {
		return task, true
	}

	digest, found := tags.digests[newTag]
	if !found {
		logger.Warnf(
			"[dockerfile] %s: the registry reports no digest for %s:%s; "+
				"leaving the digest-pinned base image unchanged",
			ref.dep.FilePath, ref.parsed.FullName(), newTag,
		)
		return upgradeTask{}, false
	}

	task.newDigest = digest
	return task, true
}

// currentRef is the reference as the Dockerfile spells it today.
func (t upgradeTask) currentRef() string {
	return withDigest(t.dep.Source+":"+t.dep.CurrentVer, t.parsed.Digest)
}

// newRef is the reference that replaces it: the new tag, plus the digest that
// tag resolves to whenever the clause pinned one, so the two halves of the
// reference cannot end up naming different images.
func (t upgradeTask) newRef() string {
	return withDigest(t.dep.Source+":"+t.newTag, t.newDigest)
}

// withDigest appends a digest to an image reference, or returns it unchanged
// when there is none to pin.
func withDigest(ref, digest string) string {
	if digest == "" {
		return ref
	}
	return ref + "@" + digest
}

// replaceRef rewrites the first whole occurrence of current, skipping any match
// that is only the leading part of a longer reference.
//
// A plain "python:3.13-slim" occurs inside "python:3.13-slim@sha256:...", and a
// substring replace would take that first match -- rewriting the digest-pinned
// clause, which belongs to a different task or to no task at all, and leaving
// the clause this task is for untouched. The same prefix trap catches
// "python:3.1" inside "python:3.13". Requiring the match to end the reference
// rules out both.
func replaceRef(content, current, replacement string) string {
	for offset := 0; offset <= len(content)-len(current); {
		index := strings.Index(content[offset:], current)
		if index < 0 {
			break
		}
		index += offset

		end := index + len(current)
		if end < len(content) && continuesReference(content[end]) {
			offset = end
			continue
		}

		return content[:index] + replacement + content[end:]
	}

	return content
}

// continuesReference reports whether a byte could extend the image reference a
// match stopped at: another tag character, or the "@" opening a digest.
func continuesReference(char byte) bool {
	switch {
	case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9':
		return true
	case char == '.', char == '_', char == '-', char == '@':
		return true
	default:
		return false
	}
}

// --- upgrade application ---

func applyUpgrades(tasks []upgradeTask, allRefs []imageRef) []entities.FileChange {
	// Build file content map from refs
	fileContent := make(map[string]string)
	for _, ref := range allRefs {
		if _, ok := fileContent[ref.dep.FilePath]; !ok {
			fileContent[ref.dep.FilePath] = ref.fileContent
		}
	}

	// Apply each upgrade
	for _, t := range tasks {
		content, ok := fileContent[t.dep.FilePath]
		if !ok {
			continue
		}
		content = replaceRef(content, t.currentRef(), t.newRef())
		fileContent[t.dep.FilePath] = content
	}

	// Collect only changed files
	originalContent := make(map[string]string)
	for _, ref := range allRefs {
		if _, ok := originalContent[ref.dep.FilePath]; !ok {
			originalContent[ref.dep.FilePath] = ref.fileContent
		}
	}

	var changes []entities.FileChange
	for path, content := range fileContent {
		if content != originalContent[path] {
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
	allRefs []imageRef,
) ([]entities.PullRequest, error) {
	branchName := generateBranchName(upgrades)

	exists, prCheckErr := provider.PullRequestExists(ctx, repo, branchName)
	if prCheckErr != nil {
		logger.Warnf("[dockerfile] Failed to check existing PRs: %v", prCheckErr)
	}
	if exists {
		logger.Infof("[dockerfile] PR already exists for branch %q, skipping", branchName)
		return []entities.PullRequest{}, nil
	}

	fileChanges := applyUpgrades(upgrades, allRefs)
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

	logger.Infof("[dockerfile] Created PR #%d for %s/%s: %s", pr.ID, repo.Organization, repo.Name, pr.URL)
	return []entities.PullRequest{*pr}, nil
}

// --- PR text generation ---

func generateBranchName(tasks []upgradeTask) string {
	if len(tasks) == 1 {
		return fmt.Sprintf(branchSingleFmt, tasks[0].parsed.Image, tasks[0].newTag)
	}
	return fmt.Sprintf(branchBatchFmt, len(tasks))
}

func generateCommitMessage(tasks []upgradeTask) string {
	if len(tasks) == 1 {
		return fmt.Sprintf(
			"chore(deps): upgraded `%s` from `%s` to `%s`",
			tasks[0].parsed.FullName(), tasks[0].dep.CurrentVer, tasks[0].newTag,
		)
	}
	return fmt.Sprintf("chore(deps): upgraded %d Docker base images", len(tasks))
}

func generatePRTitle(tasks []upgradeTask) string {
	if len(tasks) == 1 {
		return fmt.Sprintf(
			"chore(deps): upgraded `%s` to `%s`",
			tasks[0].parsed.FullName(), tasks[0].newTag,
		)
	}
	return fmt.Sprintf("chore(deps): upgraded %d Docker base images", len(tasks))
}

func generatePRDescription(tasks []upgradeTask) string {
	var sb strings.Builder
	sb.WriteString("## Summary\n\n")

	if len(tasks) <= maxDetailedUpgrades {
		sb.WriteString("This PR upgrades the following Docker base images:\n\n")
		sb.WriteString("| Image | Current Tag | New Tag | File |\n")
		sb.WriteString("|-------|-------------|---------|------|\n")
		for _, t := range tasks {
			fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n",
				t.parsed.FullName(), t.dep.CurrentVer, t.newTag, t.dep.FilePath)
		}
	} else {
		fmt.Fprintf(&sb, "This PR upgrades **%d** Docker base images.\n", len(tasks))
	}

	if anyDigestPinned(tasks) {
		sb.WriteString(
			"\nBase images pinned by digest keep their pin: the digest was re-resolved " +
				"from the registry, so it names the new tag rather than the previous image.\n",
		)
	}

	sb.WriteString("\n---\n")
	sb.WriteString("*This PR was automatically created by [autoupdate](https://github.com/rios0rios0/autoupdate)*\n")
	return sb.String()
}

// anyDigestPinned reports whether any of the upgrades rewrote a digest as well
// as a tag, which is worth calling out: a reviewer seeing a changed sha256 in
// the diff should not have to work out where it came from.
func anyDigestPinned(tasks []upgradeTask) bool {
	for _, t := range tasks {
		if t.newDigest != "" {
			return true
		}
	}
	return false
}

// appendChangelogEntry records the base image upgrades in the target repo's
// changelog and appends the resulting file to the change set. The format --
// Keep a Changelog or chlog fragments -- is decided by the shared helper from
// what the repository itself commits.
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
		entries = append(entries, fmt.Sprintf(
			"- changed the Docker base image `%s` from `%s` to `%s`",
			up.parsed.FullName(), up.dep.CurrentVer, up.newTag,
		))
	}
	return entries
}
