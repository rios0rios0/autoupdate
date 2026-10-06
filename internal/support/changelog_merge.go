package support

import (
	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// pendingStatement is one entry already waiting to be released, as the merge
// sees it: the dependencies it names, when it is a dependency statement at all.
//
// The caller lists statements in the order a reader meets them -- document
// order for a Keep a Changelog file, oldest first for chlog fragments -- which
// is the order that decides which mention survives when several name the same
// dependency.
type pendingStatement struct {
	// items is nil for a statement that is not one autoupdate may rewrite:
	// prose, a generic sentence, a fragment with keys autoupdate does not own.
	items []entities.DependencyChange
	// preferred marks a statement filed outside the default bucket, which a
	// maintainer put there on purpose -- a "Security" entry for a bump that
	// remediates a vulnerability.
	preferred bool
}

// dependencyMerge is what merging a run's changes into the pending statements
// decided.
type dependencyMerge struct {
	// rewrites maps a statement index to its new text; an empty text removes
	// the statement.
	rewrites map[int]string
	// additions are the changes no pending statement mentions, for new lines.
	additions []entities.DependencyChange
}

// mention locates one item of a pending statement.
type mention struct {
	statement, item int
}

// mergeDependencyChanges folds a run's changes into the statements already
// pending, so that a release names each dependency once, at the version it
// ships.
//
// A dependency already mentioned is updated where it is: the surviving mention
// keeps the lowest "from" any mention records -- the version the last release
// shipped -- and takes this run's "to". Any other mention of it loses the item,
// and a statement left naming nothing is removed. A dependency nothing mentions
// becomes an addition, and new names never go into the spare room of an older
// line: a statement records what one run did. Statements naming only
// dependencies this run did not move are never touched, so applying the same
// changes twice changes nothing the second time.
func mergeDependencyChanges(
	pending []pendingStatement,
	changes []entities.DependencyChange,
) dependencyMerge {
	merge := dependencyMerge{rewrites: map[int]string{}}

	items := make([][]entities.DependencyChange, len(pending))
	removed := make([][]bool, len(pending))
	for i, statement := range pending {
		items[i] = append([]entities.DependencyChange(nil), statement.items...)
		removed[i] = make([]bool, len(statement.items))
	}

	touched := map[int]bool{}
	for _, change := range changes {
		mentions := findMentions(items, removed, change.Key())
		if len(mentions) == 0 {
			merge.additions = append(merge.additions, change)
			continue
		}

		survivor := pickSurvivor(pending, mentions)
		from := lowestFrom(items, mentions, survivor)
		current := &items[survivor.statement][survivor.item]
		warnOnDowngrade(*current, change)

		for _, other := range mentions {
			if other != survivor {
				removed[other.statement][other.item] = true
				touched[other.statement] = true
			}
		}

		switch {
		case from == change.To:
			// The dependency ends the cycle where it started: nothing to state.
			removed[survivor.statement][survivor.item] = true
			touched[survivor.statement] = true
		case current.From != from || current.To != change.To:
			current.From, current.To = from, change.To
			touched[survivor.statement] = true
		}
	}

	for statement := range touched {
		merge.rewrites[statement] = renderRemaining(items[statement], removed[statement])
	}
	return merge
}

// findMentions returns every pending item still naming the dependency key.
func findMentions(items [][]entities.DependencyChange, removed [][]bool, key string) []mention {
	var mentions []mention
	for statement, statementItems := range items {
		for item, change := range statementItems {
			if !removed[statement][item] && change.Key() == key {
				mentions = append(mentions, mention{statement: statement, item: item})
			}
		}
	}
	return mentions
}

// pickSurvivor chooses the mention a dependency keeps: the first one a
// maintainer filed outside the default bucket, otherwise the first one.
func pickSurvivor(pending []pendingStatement, mentions []mention) mention {
	for _, candidate := range mentions {
		if pending[candidate.statement].preferred {
			return candidate
		}
	}
	return mentions[0]
}

// lowestFrom returns the lowest "from" among the mentions, starting from the
// survivor's own; a version that cannot be compared with it is skipped.
func lowestFrom(items [][]entities.DependencyChange, mentions []mention, survivor mention) string {
	lowest := items[survivor.statement][survivor.item].From
	for _, other := range mentions {
		from := items[other.statement][other.item].From
		if cmp, ordered := compareVersions(from, lowest); ordered && cmp < 0 {
			lowest = from
		}
	}
	return lowest
}

// warnOnDowngrade logs a run that leaves a dependency below the version a
// pending statement already records. The statement still takes this run's
// version, because that is the one the repository now builds with -- a held
// back Go requirement or a revert on the default branch moves it down for real.
func warnOnDowngrade(pending, change entities.DependencyChange) {
	if cmp, ordered := compareVersions(change.To, pending.To); ordered && cmp < 0 {
		logger.Warnf("The changelog recorded %s at %s; this run leaves it at %s",
			describeDependency(change), pending.To, change.To)
	}
}

// describeDependency names a change's dependency for a log line.
func describeDependency(change entities.DependencyChange) string {
	if change.Name == "" {
		return string(change.Subject)
	}
	return string(change.Subject) + " " + change.Name
}

// renderRemaining renders a statement from the items it still names, or ""
// when none is left.
func renderRemaining(items []entities.DependencyChange, removed []bool) string {
	kept := make([]entities.DependencyChange, 0, len(items))
	for i, item := range items {
		if !removed[i] {
			kept = append(kept, item)
		}
	}
	return entities.DependencySentence(kept)
}
