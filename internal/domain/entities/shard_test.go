package entities_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/test/domain/entitybuilders"
)

func TestParseShard(t *testing.T) {
	t.Parallel()

	valid := []struct {
		name  string
		value string
		index int
		total int
	}{
		{"the only shard", "1/1", 1, 1},
		{"the first of four", "1/4", 1, 4},
		{"one in the middle", "2/4", 2, 4},
		{"the last of four", "4/4", 4, 4},
		{"a two-digit index and total", "12/16", 12, 16},
	}
	for _, testCase := range valid {
		t.Run("should accept "+testCase.name, func(t *testing.T) {
			t.Parallel()

			// given
			value := testCase.value

			// when
			shard, err := entities.ParseShard(value)

			// then
			require.NoError(t, err)
			assert.True(t, shard.IsSet())
			assert.Equal(t, testCase.index, shard.Index())
			assert.Equal(t, testCase.total, shard.Total())
			assert.Equal(t, value, shard.String(), "it must print the way it is written")
		})
	}

	invalid := []struct {
		name  string
		value string
	}{
		{"an index of zero", "0/4"},
		{"an index past the total", "5/4"},
		{"a total of zero", "2/0"},
		{"no total", "2"},
		{"a non-numeric index", "a/4"},
		{"a non-numeric total", "2/b"},
		{"a third part", "2/4/1"},
		{"an empty index", "/4"},
		{"an empty total", "2/"},
		{"two empty sides", "/"},
		{"an empty value", ""},
		{"a negative index", "-1/4"},
		{"a signed index", "+2/4"},
		{"surrounding spaces", " 2/4 "},
		{"a fractional total", "2/4.0"},
		{"a total too large for an int", "1/99999999999999999999"},
	}
	for _, testCase := range invalid {
		t.Run("should reject "+testCase.name, func(t *testing.T) {
			t.Parallel()

			// given
			value := testCase.value

			// when
			shard, err := entities.ParseShard(value)

			// then
			require.ErrorIs(t, err, entities.ErrInvalidShard)
			assert.False(t, shard.IsSet(), "a value that names no shard must not split the run")
		})
	}
}

func TestShardSelect(t *testing.T) {
	t.Parallel()

	t.Run("should keep every repository, in the given order, when the run is not split", func(t *testing.T) {
		t.Parallel()

		// given
		shard := entities.Shard{}
		repos := shuffled(projectRepositories("project-a", 5))

		// when
		selected := shard.Select(repos)

		// then
		assert.False(t, shard.IsSet())
		assert.Empty(t, shard.String())
		assert.Equal(t, repos, selected)
	})

	// The provider lists them out of order; sorted by key, position p goes to shard p mod total + 1.
	unsorted := []string{"repo-5", "repo-2", "repo-7", "repo-1", "repo-4", "repo-6", "repo-3"}
	dealing := []struct {
		name  string
		names []string
		shard string
		want  []string
	}{
		{
			name:  "the first of three shards the first, fourth and seventh repositories",
			names: unsorted, shard: "1/3", want: []string{"repo-1", "repo-4", "repo-7"},
		},
		{
			name:  "the second of three shards the second and fifth repositories",
			names: unsorted, shard: "2/3", want: []string{"repo-2", "repo-5"},
		},
		{
			name:  "the third of three shards the third and sixth repositories",
			names: unsorted, shard: "3/3", want: []string{"repo-3", "repo-6"},
		},
		{
			// By name, "Repo-B" would sort first; by key, which is lowercase, it sorts second.
			name:  "by key rather than by name",
			names: []string{"repo-c", "Repo-B", "repo-a"}, shard: "2/2", want: []string{"Repo-B"},
		},
		{
			name:  "nothing to a shard past the last repository",
			names: []string{"repo-1", "repo-2"}, shard: "3/4", want: []string{},
		},
	}
	for _, testCase := range dealing {
		t.Run("should deal "+testCase.name, func(t *testing.T) {
			t.Parallel()

			// given
			shard := parseShard(t, testCase.shard)
			repos := namedRepositories("project-a", testCase.names...)

			// when
			selected := shard.Select(repos)

			// then
			assert.Equal(t, testCase.want, repositoryNames(selected))
		})
	}
}

func TestShardSelectSplitsEveryRepositoryOnce(t *testing.T) {
	t.Parallel()

	mixed := slices.Concat(projectRepositories("project-a", 9), projectRepositories("project-b", 5))
	for total := 1; total <= len(mixed)+2; total++ {
		t.Run(fmt.Sprintf("should split into %d disjoint, balanced shards holding every repository", total),
			func(t *testing.T) {
				t.Parallel()

				// given
				repos := mixed

				// when
				shards := selectEveryShard(t, repos, total)

				// then
				holders := shardsHolding(shards)
				assert.Len(t, holders, len(repos), "the shards must hold every repository")
				for key, indexes := range holders {
					assert.Len(t, indexes, 1, "%s must be in exactly one shard", key)
				}
				assertSpreadAtMostOne(t, shardSizes(shards), "the shard sizes")
			})
	}
}

func TestShardSelectSpreadsEveryProject(t *testing.T) {
	t.Parallel()

	t.Run("should give every shard an even slice of every project", func(t *testing.T) {
		t.Parallel()

		// given -- cut into contiguous runs instead, the first two shards would hold all of project-a
		projectA := projectRepositories("project-a", 8)
		projectB := projectRepositories("project-b", 7)
		const total = 4

		// when
		holders := shardsHolding(selectEveryShard(t, slices.Concat(projectA, projectB), total))

		// then
		assertSpreadAtMostOne(t, shareOfEachShard(holders, projectA, total), "the shares of project-a")
		assertSpreadAtMostOne(t, shareOfEachShard(holders, projectB, total), "the shares of project-b")
		for i := 1; i < len(projectA); i++ {
			previous, current := entities.RepoKey(projectA[i-1]), entities.RepoKey(projectA[i])
			assert.NotEqual(t, holders[previous], holders[current],
				"%s and %s are consecutive, so they must be in different shards", previous, current)
		}
	})
}

func TestShardSelectIgnoresListingOrder(t *testing.T) {
	t.Parallel()

	orderings := []struct {
		name  string
		order func([]entities.Repository) []entities.Repository
	}{
		{"reversed", reversed},
		{"shuffled", shuffled},
	}
	for _, ordering := range orderings {
		t.Run("should select the same repositories when the provider lists them "+ordering.name, func(t *testing.T) {
			t.Parallel()

			// given
			repos := slices.Concat(projectRepositories("project-a", 6), projectRepositories("project-b", 5))
			listing := ordering.order(repos)
			listed := slices.Clone(listing)

			// when
			shards := selectEveryShard(t, listing, 3)

			// then
			assert.Equal(t, selectEveryShard(t, repos, 3), shards)
			assert.Equal(t, listed, listing, "selecting must not reorder the caller's slice")
		})
	}
}

// parseShard parses a shard the test knows to be valid.
func parseShard(t *testing.T, value string) entities.Shard {
	t.Helper()

	shard, err := entities.ParseShard(value)
	require.NoError(t, err)
	return shard
}

// selectEveryShard splits repos into total shards and returns what each one selects, in index
// order.
func selectEveryShard(t *testing.T, repos []entities.Repository, total int) [][]entities.Repository {
	t.Helper()

	shards := make([][]entities.Repository, 0, total)
	for index := 1; index <= total; index++ {
		shards = append(shards, parseShard(t, fmt.Sprintf("%d/%d", index, total)).Select(repos))
	}
	return shards
}

// assertSpreadAtMostOne checks that the largest and the smallest of counts differ by at most one.
func assertSpreadAtMostOne(t *testing.T, counts []int, what string) {
	t.Helper()

	assert.LessOrEqual(t, slices.Max(counts)-slices.Min(counts), 1,
		"%s must differ by at most one: %v", what, counts)
}

// shardsHolding maps the key of every repository the shards hold to the indexes, counted from 0,
// of the shards holding it.
func shardsHolding(shards [][]entities.Repository) map[string][]int {
	holders := make(map[string][]int)
	for index, shard := range shards {
		for _, repo := range shard {
			key := entities.RepoKey(repo)
			holders[key] = append(holders[key], index)
		}
	}
	return holders
}

// shareOfEachShard counts how many of repos each of total shards holds.
func shareOfEachShard(holders map[string][]int, repos []entities.Repository, total int) []int {
	share := make([]int, total)
	for _, repo := range repos {
		for _, index := range holders[entities.RepoKey(repo)] {
			share[index]++
		}
	}
	return share
}

// shardSizes returns how many repositories each shard holds.
func shardSizes(shards [][]entities.Repository) []int {
	sizes := make([]int, 0, len(shards))
	for _, shard := range shards {
		sizes = append(sizes, len(shard))
	}
	return sizes
}

// projectRepositories returns count repositories of project in my-org, named repo-1 onwards.
func projectRepositories(project string, count int) []entities.Repository {
	names := make([]string, 0, count)
	for number := 1; number <= count; number++ {
		names = append(names, fmt.Sprintf("repo-%d", number))
	}
	return namedRepositories(project, names...)
}

// namedRepositories returns one repository of project in my-org for each name, in that order.
func namedRepositories(project string, names ...string) []entities.Repository {
	repos := make([]entities.Repository, 0, len(names))
	for _, name := range names {
		repos = append(repos, entitybuilders.NewRepositoryBuilder().
			WithID(project+"-"+name).
			WithOrganization("my-org").
			WithProject(project).
			WithName(name).
			BuildRepository())
	}
	return repos
}

// repositoryNames returns the names of repos, in order.
func repositoryNames(repos []entities.Repository) []string {
	names := make([]string, 0, len(repos))
	for _, repo := range repos {
		names = append(names, repo.Name)
	}
	return names
}

// reversed returns a copy of repos in the opposite order.
func reversed(repos []entities.Repository) []entities.Repository {
	copied := slices.Clone(repos)
	slices.Reverse(copied)
	return copied
}

// shuffled returns a copy of repos in a fixed shuffled order, taken alternately from the front
// and from the back, so that it differs from both the original order and its reverse, and a
// failure replays.
func shuffled(repos []entities.Repository) []entities.Repository {
	ordered := make([]entities.Repository, 0, len(repos))
	for front, back := 0, len(repos)-1; front <= back; front, back = front+1, back-1 {
		ordered = append(ordered, repos[front])
		if front < back {
			ordered = append(ordered, repos[back])
		}
	}
	return ordered
}
