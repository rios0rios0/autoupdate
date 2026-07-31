package python

import (
	"context"
	"os"
	"path/filepath"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// pythonProject records which dependency manifests a repository carries and
// which dependency manager was selected from them. Every decision that depends
// on the dependency manager — the upgrade commands, the changelog entry, the
// pull request description and the dry-run report — reads it from this one
// value, so those cannot disagree about what the run actually did.
type pythonProject struct {
	HasRequirements bool
	HasPyproject    bool
	usesPDM         bool
}

// UsesPDM reports whether the repository is upgraded through PDM.
func (p pythonProject) UsesPDM() bool { return p.usesPDM }

// Toolchain names the dependency manager the repository is upgraded with,
// either [toolchainPDM] or [toolchainPip].
func (p pythonProject) Toolchain() string {
	if p.usesPDM {
		return toolchainPDM
	}
	return toolchainPip
}

// newPythonProject is the single place that decides which dependency manager a
// repository is upgraded with, so no call site can reach that decision on its
// own and arrive somewhere different.
//
// PDM is selected only when the repository already carries a pyproject.toml.
// That file *is* PDM's project definition: running `pdm update` in a directory
// without one makes PDM write a fresh pyproject.toml, which would convert a
// pip/requirements.txt repository into a PDM repository inside what was meant
// to be a dependency bump. A repository whose only manifest is a
// requirements.txt therefore keeps pip, no matter what other PDM markers are
// lying around.
func newPythonProject(hasRequirements, hasPyproject, hasPDMMarkers bool) pythonProject {
	if hasPDMMarkers && !hasPyproject {
		logger.Warnf(
			"[python] PDM markers found but no pyproject.toml; " +
				"upgrading with pip so the repository keeps its current package manager",
		)
	}

	return pythonProject{
		HasRequirements: hasRequirements,
		HasPyproject:    hasPyproject,
		usesPDM:         hasPyproject && hasPDMMarkers,
	}
}

// detectRemoteProject inspects a repository through the provider API.
func detectRemoteProject(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) pythonProject {
	hasPyproject := provider.HasFile(ctx, repo, "pyproject.toml")

	return newPythonProject(
		provider.HasFile(ctx, repo, "requirements.txt"),
		hasPyproject,
		hasPDMRemote(ctx, provider, repo, hasPyproject),
	)
}

// detectLocalProject inspects a checked-out repository on disk.
func detectLocalProject(repoDir string) pythonProject {
	return newPythonProject(
		fileExists(filepath.Join(repoDir, "requirements.txt")),
		fileExists(filepath.Join(repoDir, "pyproject.toml")),
		hasPDMLocal(repoDir),
	)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
