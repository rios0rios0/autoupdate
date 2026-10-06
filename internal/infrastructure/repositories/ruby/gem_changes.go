package ruby

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
)

const (
	gemfileLockFileName = "Gemfile.lock"
	rubyVersionFileName = ".ruby-version"

	// rbChangelogSummary is recorded when a run moved no gem the Gemfile names
	// and nothing else is pending: only gems the lock resolves for something
	// else changed, but a pull request with no changelog change while nothing is
	// pending fails the shared checks.
	rbChangelogSummary = "- changed the transitive Ruby gem dependencies to their latest versions"

	// lockDependenciesSection lists the gems the Gemfile names itself.
	lockDependenciesSection = "DEPENDENCIES"
	// lockSpecsHeader opens the resolved gems of a GEM, GIT or PATH source.
	lockSpecsHeader = "specs:"
	// specIndent and dependencyIndent are the depths at which bundler writes a
	// resolved gem and a Gemfile dependency; anything deeper is a gem's own
	// requirement.
	specIndent       = "    "
	dependencyIndent = "  "
)

// errUnreadableGemfileLock reports a Gemfile.lock whose gems could not be read.
var errUnreadableGemfileLock = errors.New("could not read the gems of Gemfile.lock")

// lockSourceSections are the Gemfile.lock sections that resolve gems.
//
//nolint:gochecknoglobals // read-only lookup table
var lockSourceSections = map[string]bool{"GEM": true, "GIT": true, "PATH": true}

// observeGemChanges reports what an upgrade moved: the versions Gemfile.lock
// resolves for the gems the Gemfile names, and the .ruby-version pin.
func observeGemChanges(ctx context.Context, repoDir string) ([]entities.DependencyChange, error) {
	files, err := support.ReadModifiedFiles(ctx, repoDir, func(filePath string) bool {
		return filePath == gemfileLockFileName || filePath == rubyVersionFileName
	})
	if err != nil {
		return nil, err
	}

	var changes []entities.DependencyChange
	for _, file := range files {
		if file.Path == rubyVersionFileName {
			changes = append(changes, support.PinChanges(file, entities.SubjectRubyVersion, parseRubyVersionFile)...)
			continue
		}

		before, beforeErr := lockedGemVersions(file.Before)
		if beforeErr != nil {
			return nil, fmt.Errorf("%s at HEAD: %w", gemfileLockFileName, beforeErr)
		}
		after, afterErr := lockedGemVersions(file.After)
		if afterErr != nil {
			return nil, fmt.Errorf("%s: %w", gemfileLockFileName, afterErr)
		}
		changes = append(changes, support.DiffDeclaredVersions(entities.SubjectRubyGem, before, after)...)
	}
	return changes, nil
}

// lockedGemVersions maps every gem the Gemfile names to the version its
// Gemfile.lock resolves. A gem the lock resolves only because another gem
// needs it is not the repository's to state.
func lockedGemVersions(content []byte) (map[string]string, error) {
	resolved, declared := make(map[string]string), make(map[string]bool)
	section, inSpecs, sawSpecs := "", false, false

	for raw := range strings.SplitSeq(string(content), "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)

		switch {
		case trimmed == "":
			continue
		case !strings.HasPrefix(line, " "):
			section, inSpecs = trimmed, false
		case lockSourceSections[section] && trimmed == lockSpecsHeader:
			inSpecs, sawSpecs = true, true
		case lockSourceSections[section] && inSpecs && isAtIndent(line, specIndent):
			if name, version, ok := parseLockedSpec(trimmed); ok {
				resolved[name] = version
			}
		case section == lockDependenciesSection && isAtIndent(line, dependencyIndent):
			declared[lockedDependencyName(trimmed)] = true
		}
	}

	if sawSpecs && len(resolved) == 0 && len(declared) > 0 {
		return nil, errUnreadableGemfileLock
	}

	versions := make(map[string]string, len(declared))
	for name := range declared {
		if version, found := resolved[name]; found {
			versions[name] = version
		}
	}
	return versions, nil
}

// isAtIndent reports whether a line is indented by exactly indent.
func isAtIndent(line, indent string) bool {
	return strings.HasPrefix(line, indent) && !strings.HasPrefix(line, indent+" ")
}

// parseLockedSpec reads "name (version)" from a resolved gem, dropping the
// platform a native gem is built for: "nokogiri (1.16.0-x86_64-linux)" is
// nokogiri 1.16.0. RubyGems spells a pre-release with a dot, never a dash, so
// the first dash always starts the platform.
func parseLockedSpec(spec string) (string, string, bool) {
	name, rest, found := strings.Cut(spec, " (")
	version, closed := strings.CutSuffix(rest, ")")
	if !found || !closed || name == "" || version == "" {
		return "", "", false
	}
	version, _, _ = strings.Cut(version, "-")
	return name, version, true
}

// lockedDependencyName reads the gem name from a DEPENDENCIES line such as
// "rails (~> 7.1)" or "my_gem!".
func lockedDependencyName(line string) string {
	name, _, _ := strings.Cut(line, " ")
	return strings.TrimSuffix(name, "!")
}
