package csharp

import (
	"context"
	"encoding/xml"
	"fmt"
	"path"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
)

const (
	projectFileSuffix           = ".csproj"
	centralPackagesFileName     = "Directory.Packages.props"
	globalJSONFileName          = "global.json"
	msbuildPropertyReferenceTag = "$("

	// dotnetChangelogSummary is recorded when a run moved no package a project
	// file names and nothing else is pending -- only a lock file changed -- but
	// a pull request with no changelog change while nothing is pending fails the
	// shared checks.
	dotnetChangelogSummary = "- changed the transitive NuGet dependencies to their latest versions"
)

// msbuildProject is the part of a project or props file the reader needs.
type msbuildProject struct {
	ItemGroups []msbuildItemGroup `xml:"ItemGroup"`
}

// msbuildItemGroup holds the package items of one ItemGroup.
type msbuildItemGroup struct {
	References []msbuildPackage `xml:"PackageReference"`
	Versions   []msbuildPackage `xml:"PackageVersion"`
}

// msbuildPackage is a PackageReference or a central PackageVersion. The version
// is an attribute or a child element, and a project may override a centrally
// managed one.
type msbuildPackage struct {
	Include         string `xml:"Include,attr"`
	Update          string `xml:"Update,attr"`
	Version         string `xml:"Version,attr"`
	VersionOverride string `xml:"VersionOverride,attr"`
	VersionElement  string `xml:"Version"`
}

// observePackageChanges reports what an upgrade moved: the version every
// modified project file, and the central Directory.Packages.props, declares for
// each package, and the SDK pinned in global.json.
func observePackageChanges(ctx context.Context, repoDir string) ([]entities.DependencyChange, error) {
	files, err := support.ReadModifiedFiles(ctx, repoDir, func(filePath string) bool {
		base := path.Base(filePath)
		return strings.HasSuffix(base, projectFileSuffix) || base == centralPackagesFileName ||
			filePath == globalJSONFileName
	})
	if err != nil {
		return nil, err
	}

	var changes []entities.DependencyChange
	for _, file := range files {
		if file.Path == globalJSONFileName {
			changes = append(changes, support.PinChanges(file, entities.SubjectDotnetVersion, parseGlobalJSON)...)
			continue
		}

		before, beforeErr := declaredPackageVersions(file.Before)
		if beforeErr != nil {
			return nil, fmt.Errorf("%s at HEAD: %w", file.Path, beforeErr)
		}
		after, afterErr := declaredPackageVersions(file.After)
		if afterErr != nil {
			return nil, fmt.Errorf("%s: %w", file.Path, afterErr)
		}
		changes = append(changes, support.DiffDeclaredVersions(entities.SubjectNuGetPackage, before, after)...)
	}
	return changes, nil
}

// declaredPackageVersions maps every package an MSBuild file declares a
// literal version for to that version. A version spelled as an MSBuild
// property is resolved elsewhere and names nothing here.
func declaredPackageVersions(content []byte) (map[string]string, error) {
	var project msbuildProject
	if err := xml.Unmarshal(content, &project); err != nil {
		return nil, fmt.Errorf("failed to parse: %w", err)
	}

	versions := make(map[string]string)
	for _, group := range project.ItemGroups {
		for _, item := range append(append([]msbuildPackage(nil), group.References...), group.Versions...) {
			name, version := item.name(), item.version()
			if name != "" && version != "" && !strings.Contains(version, msbuildPropertyReferenceTag) {
				versions[name] = version
			}
		}
	}
	return versions, nil
}

// name returns the package an item refers to.
func (p msbuildPackage) name() string {
	if include := strings.TrimSpace(p.Include); include != "" {
		return include
	}
	return strings.TrimSpace(p.Update)
}

// version returns the version an item declares, an override first.
func (p msbuildPackage) version() string {
	for _, candidate := range []string{p.VersionOverride, p.Version, p.VersionElement} {
		if version := strings.TrimSpace(candidate); version != "" {
			return version
		}
	}
	return ""
}
