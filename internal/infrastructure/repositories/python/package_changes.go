package python

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
)

const (
	requirementsFileName  = "requirements.txt"
	pdmLockFileName       = "pdm.lock"
	pyprojectFileName     = "pyproject.toml"
	pythonVersionFileName = ".python-version"

	// pyChangelogSummary is recorded when a run moved no declared package and
	// nothing else is pending: only packages a lock file resolves on its own
	// changed, but a pull request with no changelog change while nothing is
	// pending fails the shared checks.
	pyChangelogSummary = "- changed the transitive Python dependencies to their latest versions"

	// pdmPackageHeader opens one locked package in a pdm.lock.
	pdmPackageHeader = "[[package]]"
)

// errUnreadablePDMLock reports a pdm.lock whose packages could not be read.
var errUnreadablePDMLock = errors.New("could not read the packages of pdm.lock")

// requirementNamePattern matches a distribution name as PEP 508 spells it.
var requirementNamePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$`)

// declaredRequirementPattern matches a quoted PEP 508 requirement as
// pyproject.toml lists one: a name, optional extras, then a version specifier,
// an environment marker or nothing at all.
var declaredRequirementPattern = regexp.MustCompile(
	`["']([A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)\s*(?:\[[^\]]*\])?\s*(?:[<>=!~;(][^"']*)?["']`,
)

// pdmLockStringPattern matches a top-level `key = "value"` line of a pdm.lock.
var pdmLockStringPattern = regexp.MustCompile(`^(name|version)\s*=\s*"([^"]*)"\s*$`)

// observePackageChanges reports what an upgrade moved: the pins of
// requirements.txt for a pip project, the versions pdm.lock resolves for the
// packages pyproject.toml declares for a PDM one, and the .python-version pin.
func observePackageChanges(ctx context.Context, repoDir string) ([]entities.DependencyChange, error) {
	files, err := support.ReadModifiedFiles(ctx, repoDir, func(filePath string) bool {
		switch filePath {
		case requirementsFileName, pdmLockFileName, pythonVersionFileName:
			return true
		}
		return false
	})
	if err != nil {
		return nil, err
	}

	var changes []entities.DependencyChange
	for _, file := range files {
		fileChanges, diffErr := diffPythonFile(repoDir, file)
		if diffErr != nil {
			return nil, diffErr
		}
		changes = append(changes, fileChanges...)
	}
	return changes, nil
}

// diffPythonFile compares one manifest the run modified.
func diffPythonFile(repoDir string, file support.ModifiedFile) ([]entities.DependencyChange, error) {
	switch path.Base(file.Path) {
	case requirementsFileName:
		return support.DiffDeclaredVersions(
			entities.SubjectPythonPackage, requirementPins(file.Before), requirementPins(file.After),
		), nil
	case pdmLockFileName:
		return diffPDMLock(repoDir, file)
	}
	return support.PinChanges(file, entities.SubjectPythonVersion, parsePythonVersionFile), nil
}

// diffPDMLock compares the versions pdm.lock resolves for the packages the
// project's pyproject.toml declares. Packages the lock resolves only because
// something else needs them are not the repository's to state.
func diffPDMLock(repoDir string, file support.ModifiedFile) ([]entities.DependencyChange, error) {
	before, err := pdmLockVersions(file.Before)
	if err != nil {
		return nil, fmt.Errorf("%s at HEAD: %w", pdmLockFileName, err)
	}
	after, err := pdmLockVersions(file.After)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", pdmLockFileName, err)
	}

	pyproject, err := os.ReadFile(support.WorkingFilePath(repoDir, pyprojectFileName))
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", pyprojectFileName, err)
	}
	declared := declaredRequirementNames(pyproject)

	return support.DiffDeclaredVersions(
		entities.SubjectPythonPackage, onlyDeclared(before, declared), onlyDeclared(after, declared),
	), nil
}

// requirementPins maps every package a requirements file pins exactly ("==" or
// "===") to its version. A range is not a version the repository builds with,
// and an option, a direct reference or a URL names no released version at all.
func requirementPins(content []byte) map[string]string {
	pins := make(map[string]string)
	for _, line := range requirementLines(string(content)) {
		if name, version, ok := parseRequirementPin(line); ok {
			pins[name] = version
		}
	}
	return pins
}

// requirementLines splits a requirements file into its logical lines: comments
// removed and backslash continuations joined.
func requirementLines(content string) []string {
	var (
		lines   []string
		current strings.Builder
	)
	for raw := range strings.SplitSeq(content, "\n") {
		line := strings.TrimRight(raw, "\r")
		if index := strings.Index(line, " #"); index >= 0 {
			line = line[:index]
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			line = ""
		}

		if continued, found := strings.CutSuffix(line, "\\"); found {
			current.WriteString(continued + " ")
			continue
		}
		current.WriteString(line)
		lines = append(lines, strings.TrimSpace(current.String()))
		current.Reset()
	}
	return lines
}

// parseRequirementPin reads "name[extras]==version" from one logical line,
// ignoring an environment marker and any trailing option such as --hash.
func parseRequirementPin(line string) (string, string, bool) {
	if line == "" || strings.HasPrefix(line, "-") || strings.Contains(line, "://") ||
		strings.Contains(line, " @ ") {
		return "", "", false
	}

	marked, _, _ := strings.Cut(line, ";")
	fields := strings.Fields(marked)
	if len(fields) == 0 {
		return "", "", false
	}
	requirement := fields[0]

	operator := "=="
	if strings.Contains(requirement, "===") {
		operator = "==="
	}
	name, version, found := strings.Cut(requirement, operator)
	if !found || version == "" || strings.ContainsAny(version, "<>=!~*,") {
		return "", "", false
	}

	name, _, _ = strings.Cut(name, "[")
	name = strings.TrimSpace(name)
	if !requirementNamePattern.MatchString(name) {
		return "", "", false
	}
	return name, version, true
}

// pdmLockVersions maps every package a pdm.lock resolves to its version.
//
// pdm.lock is TOML, but it is machine-written in a fixed shape: each package is
// a `[[package]]` table whose name and version are top-level string keys. A
// line scanner reads that shape without a TOML dependency, and a lock that has
// packages but yields none is reported rather than read as "nothing moved".
func pdmLockVersions(content []byte) (map[string]string, error) {
	versions := make(map[string]string)
	inPackage, packages := false, 0
	name, version := "", ""

	flush := func() {
		if name != "" && version != "" {
			versions[name] = version
		}
		name, version = "", ""
	}

	for raw := range strings.SplitSeq(string(content), "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.HasPrefix(line, "[") {
			flush()
			inPackage = strings.TrimSpace(line) == pdmPackageHeader
			if inPackage {
				packages++
			}
			continue
		}
		if !inPackage {
			continue
		}
		if match := pdmLockStringPattern.FindStringSubmatch(line); match != nil {
			if match[1] == "name" {
				name = match[2]
			} else {
				version = match[2]
			}
		}
	}
	flush()

	if packages > 0 && len(versions) == 0 {
		return nil, errUnreadablePDMLock
	}
	return versions, nil
}

// declaredRequirementNames returns the name of every requirement a
// pyproject.toml quotes. It reads the file without a TOML parser, which is why
// the result is only ever used to filter what a lock file resolved: a quoted
// string that happens to look like a requirement can only keep a package the
// lock actually resolved, never invent one.
func declaredRequirementNames(content []byte) []string {
	matches := declaredRequirementPattern.FindAllStringSubmatch(string(content), -1)
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		names = append(names, match[1])
	}
	return names
}

// onlyDeclared keeps the packages a pyproject.toml declares, matching names
// under PEP 503.
func onlyDeclared(versions map[string]string, declared []string) map[string]string {
	keys := make(map[string]bool, len(declared))
	for _, name := range declared {
		keys[pythonPackageKey(name)] = true
	}

	kept := make(map[string]string, len(versions))
	for name, version := range versions {
		if keys[pythonPackageKey(name)] {
			kept[name] = version
		}
	}
	return kept
}

// pythonPackageKey reduces a distribution name to the key the changelog
// matches Python packages on.
func pythonPackageKey(name string) string {
	return entities.DependencyChange{Subject: entities.SubjectPythonPackage, Name: name}.Key()
}
