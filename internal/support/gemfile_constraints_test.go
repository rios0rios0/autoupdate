package support_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/support"
)

// runGemfileRelax writes the given Gemfile, runs the emitted bash against it and
// returns the rewritten content.
func runGemfileRelax(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	gemfile := filepath.Join(dir, "Gemfile")
	require.NoError(t, os.WriteFile(gemfile, []byte(content), 0o600))

	script := filepath.Join(dir, "relax.sh")
	body := "#!" + bashPath(t) + "\nset -u\n" +
		support.GemfileConstraintScript() +
		"autoupdate_relax_gemfile_constraints " + shellQuote(gemfile) + "\n"
	require.NoError(t, os.WriteFile(script, []byte(body), 0o755))

	out, err := exec.Command("bash", script).CombinedOutput()
	require.NoError(t, err, "relax script failed: %s", out)

	rewritten, err := os.ReadFile(gemfile)
	require.NoError(t, err)

	return string(rewritten)
}

func TestGemfileConstraintScript(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		given string
		want  string
	}{
		{
			// The whole point: `~>` exists for its implicit ceiling, so
			// converting it to `>=` keeps the floor and drops the ceiling.
			name:  "single-quoted pessimistic constraint",
			given: "gem 'rails', '~> 6.0'\n",
			want:  "gem 'rails', '>= 6.0'\n",
		},
		{
			name:  "double-quoted pessimistic constraint",
			given: "gem \"rails\", \"~> 6.0.1\"\n",
			want:  "gem \"rails\", \">= 6.0.1\"\n",
		},
		{
			name:  "no space after the operator",
			given: "gem 'puma', '~>5.6'\n",
			want:  "gem 'puma', '>= 5.6'\n",
		},
		{
			name:  "pre-release suffix is preserved",
			given: "gem 'rails', '~> 7.1.0.beta1'\n",
			want:  "gem 'rails', '>= 7.1.0.beta1'\n",
		},
		{
			// An exact pin is the same kind of statement as a .ruby-version
			// file. Widening it would be a different decision entirely.
			name:  "exact pin is left alone",
			given: "gem 'rails', '6.0.1'\n",
			want:  "gem 'rails', '6.0.1'\n",
		},
		{
			// The repository said no in as many words.
			name:  "explicit upper bound is left alone",
			given: "gem 'rails', '< 7'\n",
			want:  "gem 'rails', '< 7'\n",
		},
		{
			name:  "already-open constraint is left alone",
			given: "gem 'rails', '>= 6.0'\n",
			want:  "gem 'rails', '>= 6.0'\n",
		},
		{
			name:  "unconstrained gem is left alone",
			given: "gem 'rails'\n",
			want:  "gem 'rails'\n",
		},
		{
			// The ceiling half of a compound constraint survives, so the
			// repository's explicit refusal still holds.
			name:  "compound keeps its explicit ceiling",
			given: "gem 'rails', '~> 6.0', '< 7'\n",
			want:  "gem 'rails', '>= 6.0', '< 7'\n",
		},
		{
			// A line that merely mentions the operator is not a constraint.
			name:  "prose mentioning the operator is untouched",
			given: "# pin with ~> when you mean it\ngem 'rails'\n",
			want:  "# pin with ~> when you mean it\ngem 'rails'\n",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// given / when
			got := runGemfileRelax(t, testCase.given)

			// then
			assert.Equal(t, testCase.want, got)
		})
	}

	t.Run("should tolerate a missing Gemfile", func(t *testing.T) {
		t.Parallel()

		// given -- a Ruby repository without a Gemfile is not an error, and the
		// surrounding script runs under `set -e`
		dir := t.TempDir()
		script := filepath.Join(dir, "relax.sh")
		body := "#!" + bashPath(t) + "\nset -eu\n" +
			support.GemfileConstraintScript() +
			"autoupdate_relax_gemfile_constraints " + shellQuote(filepath.Join(dir, "Gemfile")) + "\n" +
			"echo reached-the-end\n"
		require.NoError(t, os.WriteFile(script, []byte(body), 0o755))

		// when
		out, err := exec.Command("bash", script).CombinedOutput()

		// then
		require.NoError(t, err, "output: %s", out)
		assert.Contains(t, string(out), "reached-the-end")
	})
}
