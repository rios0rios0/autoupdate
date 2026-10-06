package javascript

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	packageLockFileName = "package-lock.json"
	pnpmLockFileName    = "pnpm-lock.yaml"
	yarnLockFileName    = "yarn.lock"

	// rootImporter is the importer pnpm files the root project under.
	rootImporter = "."
	// yarnFieldIndent is the depth of an entry's own fields in a yarn.lock;
	// anything deeper belongs to its dependency list.
	yarnFieldIndent = "  "
	// yarnNpmProtocol is how Berry spells a registry descriptor.
	yarnNpmProtocol = "npm:"
)

// errUnreadableLockfile reports a lockfile whose entries could not be read.
var errUnreadableLockfile = errors.New("could not read the entries of the lockfile")

// lockfileResolver maps the dependencies one package.json declares -- name to
// range -- to the versions a lockfile resolves for them.
type lockfileResolver func(lock []byte, dir string, declared map[string]string) (map[string]string, error)

// lockfileResolvers picks the resolver by lockfile name, so adding a package
// manager adds an entry rather than a branch.
//
//nolint:gochecknoglobals // read-only lookup table
var lockfileResolvers = map[string]lockfileResolver{
	packageLockFileName: resolvePackageLock,
	pnpmLockFileName:    resolvePnpmLock,
	yarnLockFileName:    resolveYarnLock,
}

// pnpmProjectKeys are the top-level pnpm-lock.yaml sections that list what a
// project declares; everything else -- the resolved packages and snapshots,
// most of a large lockfile -- is skipped before decoding.
//
//nolint:gochecknoglobals // read-only lookup table
var pnpmProjectKeys = map[string]bool{
	"importers": true, "dependencies": true, "devDependencies": true, "optionalDependencies": true,
}

// npmLockfile is the part of a package-lock.json the reader needs: the
// lockfile v2/v3 "packages" map, or the v1 "dependencies" map.
type npmLockfile struct {
	Packages     map[string]npmLockedPackage `json:"packages"`
	Dependencies map[string]npmLockedPackage `json:"dependencies"`
}

// npmLockedPackage is one package a package-lock.json resolves.
type npmLockedPackage struct {
	Version string `json:"version"`
}

// pnpmLockfile is the part of a pnpm-lock.yaml the reader needs. Lockfile v6
// and later file every project under "importers"; a v5 single-project lockfile
// lists them at the top level.
type pnpmLockfile struct {
	Importers            map[string]pnpmImporter `yaml:"importers"`
	Dependencies         map[string]yaml.Node    `yaml:"dependencies"`
	DevDependencies      map[string]yaml.Node    `yaml:"devDependencies"`
	OptionalDependencies map[string]yaml.Node    `yaml:"optionalDependencies"`
}

// pnpmImporter is what one project declares. A dependency is a version string
// (v5) or a mapping carrying "specifier" and "version" (v6 and later).
type pnpmImporter struct {
	Dependencies         map[string]yaml.Node `yaml:"dependencies"`
	DevDependencies      map[string]yaml.Node `yaml:"devDependencies"`
	OptionalDependencies map[string]yaml.Node `yaml:"optionalDependencies"`
}

// resolvePackageLock reads package-lock.json: a workspace's own copy of a
// package first, then the hoisted one, or the v1 top-level entry.
func resolvePackageLock(lock []byte, dir string, declared map[string]string) (map[string]string, error) {
	var lockfile npmLockfile
	if err := json.Unmarshal(lock, &lockfile); err != nil {
		return nil, fmt.Errorf("failed to parse: %w", err)
	}
	if lockfile.Packages == nil && lockfile.Dependencies == nil {
		return nil, errUnreadableLockfile
	}

	versions := make(map[string]string, len(declared))
	for name := range declared {
		if version := lockfile.versionOf(dir, name); version != "" {
			versions[name] = version
		}
	}
	return versions, nil
}

// versionOf returns the version the lockfile resolves for a package the
// project in dir declares.
func (l npmLockfile) versionOf(dir, name string) string {
	if l.Packages == nil {
		return l.Dependencies[name].Version
	}
	if dir != rootImporter {
		if nested, found := l.Packages[dir+"/node_modules/"+name]; found && nested.Version != "" {
			return nested.Version
		}
	}
	return l.Packages["node_modules/"+name].Version
}

// resolvePnpmLock reads pnpm-lock.yaml: the importer of the project in dir,
// with the peer suffix pnpm appends to a version dropped.
func resolvePnpmLock(lock []byte, dir string, declared map[string]string) (map[string]string, error) {
	var lockfile pnpmLockfile
	if err := yaml.Unmarshal(pnpmProjectSections(lock), &lockfile); err != nil {
		return nil, fmt.Errorf("failed to parse: %w", err)
	}

	importer := lockfile.importer(dir)
	versions := make(map[string]string, len(declared))
	for name := range declared {
		if version := importer.versionOf(name); version != "" {
			versions[name] = version
		}
	}
	return versions, nil
}

// importer returns what the project in dir declares.
func (l pnpmLockfile) importer(dir string) pnpmImporter {
	if len(l.Importers) > 0 {
		return l.Importers[dir]
	}
	return pnpmImporter{
		Dependencies:         l.Dependencies,
		DevDependencies:      l.DevDependencies,
		OptionalDependencies: l.OptionalDependencies,
	}
}

// versionOf returns the release the importer resolves a dependency to, or ""
// for one that is linked, a path or a repository rather than a release.
func (i pnpmImporter) versionOf(name string) string {
	for _, section := range []map[string]yaml.Node{i.Dependencies, i.DevDependencies, i.OptionalDependencies} {
		node, found := section[name]
		if !found {
			continue
		}

		version := pnpmNodeVersion(&node)
		version, _, _ = strings.Cut(version, "(")
		version, _, _ = strings.Cut(version, "_")
		if strings.ContainsAny(version, ":/") {
			return ""
		}
		return version
	}
	return ""
}

// pnpmNodeVersion reads a dependency's version from its v5 string or its v6
// mapping.
func pnpmNodeVersion(node *yaml.Node) string {
	if node.Kind == yaml.ScalarNode {
		return node.Value
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == "version" {
			return node.Content[i+1].Value
		}
	}
	return ""
}

// pnpmProjectSections keeps only the top-level sections of a pnpm-lock.yaml
// that list what projects declare, so a multi-megabyte lockfile is not decoded
// whole on every run.
func pnpmProjectSections(lock []byte) []byte {
	var kept bytes.Buffer
	keep := false
	for line := range bytes.SplitSeq(lock, []byte("\n")) {
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' && line[0] != '#' {
			key, _, _ := strings.Cut(string(line), ":")
			keep = pnpmProjectKeys[strings.Trim(key, `"'`)]
		}
		if keep {
			kept.Write(line)
			kept.WriteByte('\n')
		}
	}
	return kept.Bytes()
}

// resolveYarnLock reads yarn.lock, Classic or Berry: each entry lists the
// descriptors it satisfies, so a declared dependency resolves through the
// descriptor its own range spells.
func resolveYarnLock(lock []byte, _ string, declared map[string]string) (map[string]string, error) {
	entries := yarnLockEntries(lock)
	if len(entries) == 0 && hasYarnEntries(lock) {
		return nil, errUnreadableLockfile
	}

	versions := make(map[string]string, len(declared))
	for name, versionRange := range declared {
		for _, descriptor := range []string{name + "@" + versionRange, name + "@" + yarnNpmProtocol + versionRange} {
			if version, found := entries[descriptor]; found {
				versions[name] = version
				break
			}
		}
	}
	return versions, nil
}

// yarnLockEntries maps every descriptor a yarn.lock lists to the version its
// entry resolves.
func yarnLockEntries(lock []byte) map[string]string {
	entries := make(map[string]string)
	var descriptors []string

	for raw := range strings.SplitSeq(string(lock), "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			descriptors = yarnDescriptors(line)
			continue
		}
		if !strings.HasPrefix(line, yarnFieldIndent) || strings.HasPrefix(line, yarnFieldIndent+" ") {
			continue
		}

		if version, ok := yarnVersionField(strings.TrimSpace(line)); ok {
			for _, descriptor := range descriptors {
				entries[descriptor] = version
			}
		}
	}
	return entries
}

// yarnDescriptors splits an entry header -- `"a@^1", "a@^1.2":` in Classic,
// `"a@npm:^1, a@npm:^1.2":` in Berry -- into its descriptors.
func yarnDescriptors(header string) []string {
	header = strings.ReplaceAll(strings.TrimSuffix(strings.TrimSpace(header), ":"), `"`, "")
	parts := strings.Split(header, ", ")
	descriptors := make([]string, 0, len(parts))
	for _, part := range parts {
		if descriptor := strings.TrimSpace(part); descriptor != "" {
			descriptors = append(descriptors, descriptor)
		}
	}
	return descriptors
}

// yarnVersionField reads an entry's version: `version "1.2.3"` in Classic,
// `version: 1.2.3` in Berry.
func yarnVersionField(field string) (string, bool) {
	value, found := strings.CutPrefix(field, "version")
	if !found || value == "" || (value[0] != ' ' && value[0] != ':') {
		return "", false
	}
	value = strings.Trim(strings.TrimSpace(strings.TrimPrefix(value, ":")), `"`)
	return value, value != ""
}

// hasYarnEntries reports whether a yarn.lock holds any entry header at all, so
// an empty lockfile reads as empty rather than as unreadable.
func hasYarnEntries(lock []byte) bool {
	for line := range strings.SplitSeq(string(lock), "\n") {
		if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#") &&
			strings.TrimSpace(line) != "" {
			return true
		}
	}
	return false
}
