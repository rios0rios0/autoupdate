package entities

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// ErrInvalidShard is returned by ParseShard for a value that names no shard of a run.
var ErrInvalidShard = errors.New("invalid shard")

// shardDigits are the only characters either side of a shard may hold.
const shardDigits = "0123456789"

// Shard is one of the parallel jobs a run is split across: the Index-th of Total, counted from
// 1 the way CI systems number their parallel jobs (Azure Pipelines' System.JobPositionInPhase,
// GitLab's CI_NODE_INDEX). The zero value is the whole run, not split at all.
//
// The jobs of a split run do not talk to each other. Each one lists the same repositories and
// keeps its own slice of them, which is only safe because Select hands every repository to
// exactly one shard whatever order the provider listed them in.
type Shard struct {
	index int
	total int
}

// ParseShard reads a shard written as "<index>/<total>": two decimal integers, with
// 1 <= index <= total. Anything else is an error rather than the whole run, because a job that
// cannot tell which slice is its own must not guess: processing everything would repeat the
// work of every other job.
func ParseShard(value string) (Shard, error) {
	indexText, totalText, found := strings.Cut(value, "/")
	index, indexRead := parseShardNumber(indexText)
	total, totalRead := parseShardNumber(totalText)
	if !found || !indexRead || !totalRead {
		return Shard{}, fmt.Errorf(
			"%w %q: write it as <index>/<total>, two whole numbers such as 2/4", ErrInvalidShard, value,
		)
	}

	if total < 1 {
		return Shard{}, fmt.Errorf("%w %q: the total must be at least 1", ErrInvalidShard, value)
	}
	if index < 1 || index > total {
		return Shard{}, fmt.Errorf(
			"%w %q: the index must be between 1 and the total, %d", ErrInvalidShard, value, total,
		)
	}

	return Shard{index: index, total: total}, nil
}

// IsSet reports whether the run is split at all; the zero Shard is the whole run.
func (s Shard) IsSet() bool {
	return s.total > 0
}

// Index is the shard's position among Total, counted from 1, or 0 for the whole run.
func (s Shard) Index() int {
	return s.index
}

// Total is how many shards the run is split into, or 0 for the whole run.
func (s Shard) Total() int {
	return s.total
}

// String writes the shard the way ParseShard reads it, and the whole run as "".
func (s Shard) String() string {
	if !s.IsSet() {
		return ""
	}
	return strconv.Itoa(s.index) + "/" + strconv.Itoa(s.total)
}

// Select returns the repositories this shard processes, leaving repos itself untouched.
//
// The whole run keeps every repository, in the order given. A shard sorts them by RepoKey and
// deals them out like cards: the repository at sorted position p (counted from 0) belongs to
// shard p mod Total + 1. So the shards are disjoint, together they are exactly repos, their
// sizes differ by at most one, and every job computes the same split on its own, whatever
// order its provider listed the repositories in.
//
// Dealing rather than cutting the sorted list into Total runs is deliberate. Providers list
// repositories project by project, and the cost of a run concentrates in some projects:
// contiguous runs would hand a whole expensive project to one or two shards, while dealing
// gives every shard an even slice of every project.
func (s Shard) Select(repos []Repository) []Repository {
	if !s.IsSet() {
		return repos
	}

	sorted := slices.Clone(repos)
	slices.SortFunc(sorted, compareForShard)

	selected := make([]Repository, 0, len(sorted)/s.total+1)
	for position := s.index - 1; position < len(sorted); position += s.total {
		selected = append(selected, sorted[position])
	}
	return selected
}

// parseShardNumber reads one side of a shard. Only ASCII digits are accepted, so a sign, a
// space or an empty side is refused rather than read the way [strconv.Atoi] would read it.
func parseShardNumber(text string) (int, bool) {
	if text == "" || strings.TrimLeft(text, shardDigits) != "" {
		return 0, false
	}
	number, err := strconv.Atoi(text)
	return number, err == nil
}

// compareForShard orders repositories by RepoKey, which no provider reorders, and two that
// share a key by the provider's ID, so that no tie is left for the listing order to break.
func compareForShard(a, b Repository) int {
	return cmp.Or(strings.Compare(RepoKey(a), RepoKey(b)), strings.Compare(a.ID, b.ID))
}
