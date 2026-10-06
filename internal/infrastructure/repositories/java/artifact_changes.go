package java

import (
	"context"
	"encoding/xml"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
)

const (
	pomFileName                 = "pom.xml"
	gradleWrapperPropertiesPath = "gradle/wrapper/gradle-wrapper.properties"
	javaVersionFileName         = ".java-version"

	// javaChangelogSummary is recorded when a run moved no artifact a build file
	// names and nothing else is pending -- a Gradle run that only refreshed its
	// dependency locks, say -- but a pull request with no changelog change while
	// nothing is pending fails the shared checks.
	javaChangelogSummary = "- changed the transitive Java dependencies to their latest versions"

	// defaultPluginGroup is the groupId Maven assumes for a plugin that names
	// none.
	defaultPluginGroup = "org.apache.maven.plugins"

	// maxPropertyDepth bounds how many times a version is expanded, so a
	// property defined in terms of itself cannot loop.
	maxPropertyDepth = 5
)

// gradleDistributionPattern matches the version in a Gradle wrapper's
// distributionUrl ("gradle-8.10.2-bin.zip").
var gradleDistributionPattern = regexp.MustCompile(`gradle-([0-9][0-9A-Za-z.-]*?)-(?:bin|all)\.zip`)

// mavenPropertyPattern matches one "${name}" reference.
var mavenPropertyPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// mavenProject is the part of a pom.xml the reader needs.
type mavenProject struct {
	Parent              mavenArtifact   `xml:"parent"`
	Properties          mavenProperties `xml:"properties"`
	Dependencies        []mavenArtifact `xml:"dependencies>dependency"`
	ManagedDependencies []mavenArtifact `xml:"dependencyManagement>dependencies>dependency"`
	Plugins             []mavenArtifact `xml:"build>plugins>plugin"`
	ManagedPlugins      []mavenArtifact `xml:"build>pluginManagement>plugins>plugin"`
	Profiles            []mavenProfile  `xml:"profiles>profile"`
}

// mavenProfile is the part of a profile that can declare artifacts.
type mavenProfile struct {
	Properties          mavenProperties `xml:"properties"`
	Dependencies        []mavenArtifact `xml:"dependencies>dependency"`
	ManagedDependencies []mavenArtifact `xml:"dependencyManagement>dependencies>dependency"`
	Plugins             []mavenArtifact `xml:"build>plugins>plugin"`
}

// mavenArtifact is a dependency, a plugin or the parent.
type mavenArtifact struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
}

// mavenProperties holds the free-form children of <properties>.
type mavenProperties struct {
	Entries []mavenProperty `xml:",any"`
}

// mavenProperty is one property, named by its element.
type mavenProperty struct {
	XMLName xml.Name `xml:""`
	Value   string   `xml:",chardata"`
}

// observeArtifactChanges reports what an upgrade moved: the version every
// modified pom.xml declares for each artifact, the Gradle wrapper version, and
// the .java-version pin. A Gradle build's own versions are never rewritten, and
// its dependency locks resolve transitive artifacts too, so they are not named.
func observeArtifactChanges(ctx context.Context, repoDir string) ([]entities.DependencyChange, error) {
	files, err := support.ReadModifiedFiles(ctx, repoDir, func(filePath string) bool {
		return path.Base(filePath) == pomFileName ||
			filePath == gradleWrapperPropertiesPath || filePath == javaVersionFileName
	})
	if err != nil {
		return nil, err
	}

	var changes []entities.DependencyChange
	for _, file := range files {
		fileChanges, diffErr := diffJavaFile(file)
		if diffErr != nil {
			return nil, diffErr
		}
		changes = append(changes, fileChanges...)
	}
	return changes, nil
}

// diffJavaFile compares one build file the run modified.
func diffJavaFile(file support.ModifiedFile) ([]entities.DependencyChange, error) {
	switch file.Path {
	case javaVersionFileName:
		return support.PinChanges(file, entities.SubjectJavaVersion, parseJavaVersionFile), nil
	case gradleWrapperPropertiesPath:
		return support.PinChanges(file, entities.SubjectGradleVersion, gradleWrapperVersion), nil
	}

	before, err := declaredArtifactVersions(file.Before)
	if err != nil {
		return nil, fmt.Errorf("%s at HEAD: %w", file.Path, err)
	}
	after, err := declaredArtifactVersions(file.After)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file.Path, err)
	}
	return support.DiffDeclaredVersions(entities.SubjectMavenDependency, before, after), nil
}

// declaredArtifactVersions maps every artifact a pom.xml declares a version for
// -- the parent, dependencies, managed dependencies and plugins, in profiles
// too -- to that version, keyed "groupId:artifactId". A version that is a
// property reference is resolved against the file's own properties; one that
// stays unresolved, or is absent because another POM manages it, names nothing.
func declaredArtifactVersions(content []byte) (map[string]string, error) {
	var project mavenProject
	if err := xml.Unmarshal(content, &project); err != nil {
		return nil, fmt.Errorf("failed to parse: %w", err)
	}

	properties := project.Properties.values()
	declared := []mavenArtifact{project.Parent}
	declared = append(declared, project.Dependencies...)
	declared = append(declared, project.ManagedDependencies...)
	plugins := append(append([]mavenArtifact(nil), project.Plugins...), project.ManagedPlugins...)

	for _, profile := range project.Profiles {
		for name, value := range profile.Properties.values() {
			if _, defined := properties[name]; !defined {
				properties[name] = value
			}
		}
		declared = append(declared, profile.Dependencies...)
		declared = append(declared, profile.ManagedDependencies...)
		plugins = append(plugins, profile.Plugins...)
	}

	versions := make(map[string]string)
	collectArtifactVersions(versions, declared, "", properties)
	collectArtifactVersions(versions, plugins, defaultPluginGroup, properties)
	return versions, nil
}

// collectArtifactVersions records the resolved version of every artifact that
// declares one.
func collectArtifactVersions(
	versions map[string]string,
	artifacts []mavenArtifact,
	defaultGroup string,
	properties map[string]string,
) {
	for _, artifact := range artifacts {
		group := strings.TrimSpace(artifact.GroupID)
		if group == "" {
			group = defaultGroup
		}
		name := strings.TrimSpace(artifact.ArtifactID)
		version := resolveMavenProperties(strings.TrimSpace(artifact.Version), properties)
		if group == "" || name == "" || version == "" || strings.Contains(version, "${") {
			continue
		}
		versions[group+":"+name] = version
	}
}

// values maps each property to its trimmed value.
func (p mavenProperties) values() map[string]string {
	values := make(map[string]string, len(p.Entries))
	for _, entry := range p.Entries {
		values[entry.XMLName.Local] = strings.TrimSpace(entry.Value)
	}
	return values
}

// resolveMavenProperties expands the "${name}" references in a version from
// the given properties, leaving a reference it cannot resolve in place.
func resolveMavenProperties(version string, properties map[string]string) string {
	for range maxPropertyDepth {
		if !strings.Contains(version, "${") {
			return version
		}
		version = mavenPropertyPattern.ReplaceAllStringFunc(version, func(reference string) string {
			if value, defined := properties[reference[2:len(reference)-1]]; defined {
				return value
			}
			return reference
		})
	}
	return version
}

// gradleWrapperVersion returns the Gradle version a gradle-wrapper.properties
// distributes, or "" when its distributionUrl names none.
func gradleWrapperVersion(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found || strings.TrimSpace(key) != "distributionUrl" {
			continue
		}
		if match := gradleDistributionPattern.FindStringSubmatch(value); match != nil {
			return match[1]
		}
	}
	return ""
}
