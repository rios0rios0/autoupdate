package golang

import (
	"context"
	"fmt"
	"path"

	"golang.org/x/mod/modfile"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
)

// goChangelogSummary is recorded when a run moved no requirement and no go
// directive -- only go.sum or a vendored copy changed -- and nothing else is
// pending either. There is no dependency to name, but a pull request that adds
// no changelog change while nothing is pending fails the shared checks.
const goChangelogSummary = "- changed the Go module checksums to match the declared versions"

// observeModuleChanges reports what an upgrade moved in every go.mod it
// modified: each requirement whose version changed -- `// indirect` ones
// included, because go.mod declares them -- and the go directive, as the Go
// version pin. The same requirement moving in several modules is reported once
// per module; the changelog writer folds them.
func observeModuleChanges(ctx context.Context, repoDir string) ([]entities.DependencyChange, error) {
	files, err := support.ReadModifiedFiles(ctx, repoDir, func(modPath string) bool {
		return path.Base(modPath) == goModFileName && !isSkippedModulePath(modPath)
	})
	if err != nil {
		return nil, err
	}

	var changes []entities.DependencyChange
	for _, file := range files {
		moduleChanges, diffErr := diffGoMod(file.Path, file.Before, file.After)
		if diffErr != nil {
			return nil, diffErr
		}
		changes = append(changes, moduleChanges...)
	}
	return changes, nil
}

// diffGoMod compares the requirements and the go directive of two versions of
// a go.mod.
//
// The files are parsed leniently, as the go command parses a dependency's
// go.mod: a directive this program does not know yet is skipped rather than
// failing the comparison, and toolchain, replace and exclude directives are not
// dependency upgrades.
func diffGoMod(modPath string, before, after []byte) ([]entities.DependencyChange, error) {
	beforeFile, err := modfile.ParseLax(modPath, before, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s at HEAD: %w", modPath, err)
	}
	afterFile, err := modfile.ParseLax(modPath, after, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", modPath, err)
	}

	changes := support.DiffDeclaredVersions(
		entities.SubjectGoModule, requirementVersions(beforeFile), requirementVersions(afterFile),
	)

	beforeGo, afterGo := goDirectiveOf(beforeFile), goDirectiveOf(afterFile)
	if beforeGo != "" && afterGo != "" && beforeGo != afterGo {
		changes = append(changes, entities.DependencyChange{
			Subject: entities.SubjectGoVersion, From: beforeGo, To: afterGo,
		})
	}
	return changes, nil
}

// requirementVersions maps every module a go.mod requires to its version.
func requirementVersions(file *modfile.File) map[string]string {
	versions := make(map[string]string, len(file.Require))
	for _, requirement := range file.Require {
		versions[requirement.Mod.Path] = requirement.Mod.Version
	}
	return versions
}

// goDirectiveOf returns the version a go.mod's go directive declares, or "".
func goDirectiveOf(file *modfile.File) string {
	if file.Go == nil {
		return ""
	}
	return file.Go.Version
}
