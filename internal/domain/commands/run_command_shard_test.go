package commands_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/commands"
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	doubles "github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

func TestRunCommandShard(t *testing.T) {
	t.Parallel()

	// setupRun's provider lists repo-0 to repo-5, whose keys sort in that order, so the shard
	// with index i of 3 is dealt the repositories at positions i-1 and i+2.
	cases := []struct {
		name    string
		shard   string
		exclude []string
		want    []string
	}{
		{
			name: "the only shard", shard: "1/1",
			want: []string{"repo-0", "repo-1", "repo-2", "repo-3", "repo-4", "repo-5"},
		},
		{name: "the first of three", shard: "1/3", want: []string{"repo-0", "repo-3"}},
		{name: "the second of three", shard: "2/3", want: []string{"repo-1", "repo-4"}},
		{name: "the third of three", shard: "3/3", want: []string{"repo-2", "repo-5"}},
		{name: "a shard past the last repository", shard: "7/8", want: []string{}},
		{
			// Dealt before the exclusions, the first of two would be left with repo-2 and repo-4.
			name: "the first of two, dealt from what the exclusions leave", shard: "1/2",
			exclude: []string{"repo-0"}, want: []string{"repo-1", "repo-3", "repo-5"},
		},
	}
	for _, testCase := range cases {
		t.Run("should process only the repositories of "+testCase.name, func(t *testing.T) {
			t.Parallel()

			// given
			cmd, updaterSpy, settings := setupRun(6)
			settings.ExcludeRepos = testCase.exclude
			shard, err := entities.ParseShard(testCase.shard)
			require.NoError(t, err)

			// when
			err = cmd.Execute(context.Background(), settings, commands.RunOptions{Shard: shard})

			// then
			require.NoError(t, err)
			assert.ElementsMatch(t, testCase.want, detectedNames(updaterSpy))
		})
	}
}

// detectedNames returns the names of the repositories the updater was asked to detect, which
// are the repositories the run processed.
func detectedNames(updaterSpy *doubles.SpyUpdaterRepository) []string {
	names := make([]string, 0, len(updaterSpy.DetectedRepos))
	for _, repo := range updaterSpy.DetectedRepos {
		names = append(names, repo.Name)
	}
	return names
}
