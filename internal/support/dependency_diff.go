package support

import (
	"sort"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// DiffDeclaredVersions compares the versions one manifest declares before and
// after a run, returning a change for every dependency declared on both sides
// at a different version, sorted by name.
//
// A dependency present on one side only is not an upgrade -- the run added or
// removed it -- and is left out. Names are matched the way the subject matches
// them -- case-insensitively, or under PEP 503 for Python packages -- and a
// change carries the spelling the manifest uses after the run.
func DiffDeclaredVersions(
	subject entities.DependencySubject,
	before, after map[string]string,
) []entities.DependencyChange {
	beforeByKey := make(map[string]string, len(before))
	for name, version := range before {
		beforeByKey[entities.DependencyChange{Subject: subject, Name: name}.Key()] = version
	}

	var changes []entities.DependencyChange
	for name, to := range after {
		from, declared := beforeByKey[entities.DependencyChange{Subject: subject, Name: name}.Key()]
		if !declared || from == to {
			continue
		}
		changes = append(changes, entities.DependencyChange{
			Subject: subject, Name: name, From: from, To: to,
		})
	}

	sort.Slice(changes, func(i, j int) bool { return changes[i].Name < changes[j].Name })
	return changes
}

// PinChanges compares one version pin file a run modified, returning the pin's
// change, or nothing when it did not move or either side holds no version parse
// can read -- "lts/*" or "system" is a repository's own choice, not a version.
func PinChanges(
	file ModifiedFile,
	subject entities.DependencySubject,
	parse func(content string) string,
) []entities.DependencyChange {
	from, to := parse(string(file.Before)), parse(string(file.After))
	if from == "" || to == "" || from == to {
		return nil
	}
	return []entities.DependencyChange{{Subject: subject, From: from, To: to}}
}

// FoldDependencyChanges collapses the changes that name the same dependency --
// one per module of a multi-module repository, one per Dockerfile pinning the
// same image -- into a single change from the lowest version to the highest,
// keeping the order in which each dependency first appears.
//
// A pair of versions that cannot be compared keeps the first one seen, and a
// change that ends where it started is dropped.
func FoldDependencyChanges(changes []entities.DependencyChange) []entities.DependencyChange {
	folded := make([]entities.DependencyChange, 0, len(changes))
	index := make(map[string]int, len(changes))

	for _, change := range changes {
		key := change.Key()
		position, seen := index[key]
		if !seen {
			index[key] = len(folded)
			folded = append(folded, change)
			continue
		}

		existing := &folded[position]
		if cmp, ordered := compareVersions(change.From, existing.From); ordered && cmp < 0 {
			existing.From = change.From
		}
		if cmp, ordered := compareVersions(change.To, existing.To); ordered && cmp > 0 {
			existing.To = change.To
		}
	}

	result := folded[:0]
	for _, change := range folded {
		if change.From != change.To {
			result = append(result, change)
		}
	}
	return result
}

// compareVersions orders two versions, reporting whether they could be compared
// at all. It follows the same precedence as [IsNewerVersion] -- numeric release
// segments, then semver pre-release rules, build metadata ignored -- which also
// covers Go's pseudo-versions.
func compareVersions(a, b string) (int, bool) {
	aRelease, aPre, aOK := parseVersion(a)
	bRelease, bPre, bOK := parseVersion(b)
	if !aOK || !bOK {
		return 0, a == b
	}

	if cmp := compareRelease(aRelease, bRelease); cmp != 0 {
		return cmp, true
	}
	return comparePrerelease(aPre, bPre), true
}
