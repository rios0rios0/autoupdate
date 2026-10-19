package pipeline_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/pipeline"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

// newActionRemote serves the repository an action is published from out of root, at
// root/<owner>/<repo>.git, with one commit carrying lightweight and annotated tags.
func newActionRemote(t *testing.T, root, owner, repo string, lightweight, annotated []string) {
	t.Helper()

	workDir := t.TempDir()
	source, err := git.PlainInit(workDir, false)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "action.yml"), []byte("name: 'action'\n"), 0o600))
	worktree, err := source.Worktree()
	require.NoError(t, err)
	_, err = worktree.Add("action.yml")
	require.NoError(t, err)
	signature := &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()}
	head, err := worktree.Commit("initial commit", &git.CommitOptions{Author: signature})
	require.NoError(t, err)
	for _, tag := range lightweight {
		_, err = source.CreateTag(tag, head, nil)
		require.NoError(t, err)
	}
	for _, tag := range annotated {
		_, err = source.CreateTag(tag, head, &git.CreateTagOptions{Tagger: signature, Message: "release " + tag})
		require.NoError(t, err)
	}

	bareDir := filepath.Join(root, owner, repo+".git")
	_, err = git.PlainInit(bareDir, true)
	require.NoError(t, err)
	_, err = source.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{bareDir}})
	require.NoError(t, err)
	require.NoError(t, source.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{"refs/heads/*:refs/heads/*", "refs/tags/*:refs/tags/*"},
	}))
}

func TestListRemoteTags(t *testing.T) {
	t.Parallel()

	t.Run("should list lightweight and annotated tags once each", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		newActionRemote(t, root, "actions", "checkout", []string{"v4.0.0"}, []string{"v5.0.0"})

		// when
		tags, err := pipeline.ListRemoteTags(t.Context(), filepath.Join(root, "actions", "checkout.git"))

		// then the peeled entry of the annotated tag is not a tag of its own
		require.NoError(t, err)
		slices.Sort(tags)
		assert.Equal(t, []string{"v4.0.0", "v5.0.0"}, tags)
	})

	t.Run("should fail when the remote cannot be reached", func(t *testing.T) {
		t.Parallel()

		// given / when
		_, err := pipeline.ListRemoteTags(t.Context(), filepath.Join(t.TempDir(), "missing.git"))

		// then
		require.Error(t, err)
	})
}

// TestFindActionUpgradesInFileOutsideGitHub is not parallel: it points the GitHub git
// endpoint, a package-level setting, at local repositories.
//
//nolint:paralleltest // it and its subtests change the package-level githubGitURLFormat
func TestFindActionUpgradesInFileOutsideGitHub(t *testing.T) {
	t.Run("should read the tags of an action from GitHub when the repository is hosted elsewhere", func(t *testing.T) {
		// given a workflow kept on Azure DevOps, whose provider would answer for a
		// repository of its own if it were asked
		root := t.TempDir()
		newActionRemote(t, root, "actions", "checkout", []string{"v3.0.0", "v4.0.0"}, []string{"v5.0.0"})
		pipeline.SetGitHubGitURLFormat(t, filepath.Join(root, "%s", "%s.git"))
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithProviderName("azuredevops").
			WithTags([]string{"v99.0.0"}).
			BuildSpy()
		content := "steps:\n  - uses: actions/checkout@v3\n"

		// when
		tasks := pipeline.FindActionUpgradesInFile(
			t.Context(), provider, content, ".github/workflows/deploy.yml", make(pipeline.ActionTagCache), true,
		)

		// then the upgrade comes from the action's own tags, not from the hosting provider
		require.Len(t, tasks, 1)
		assert.Equal(t, "v5", pipeline.UpgradeTaskNewVersion(tasks[0]))
	})

	//nolint:paralleltest // the parent mutates package-level githubGitURLFormat
	t.Run("should keep asking the provider when the repository is hosted on GitHub", func(t *testing.T) {
		// given a GitHub endpoint that does not exist, which must not be reached
		pipeline.SetGitHubGitURLFormat(t, filepath.Join(t.TempDir(), "%s", "%s.git"))
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithTags([]string{"v4.0.0", "v6.0.0"}).
			BuildSpy()
		content := "steps:\n  - uses: actions/checkout@v4\n"

		// when
		tasks := pipeline.FindActionUpgradesInFile(
			t.Context(), provider, content, ".github/workflows/ci.yml", make(pipeline.ActionTagCache), true,
		)

		// then
		require.Len(t, tasks, 1)
		assert.Equal(t, "v6", pipeline.UpgradeTaskNewVersion(tasks[0]))
	})
}
