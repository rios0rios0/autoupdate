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

// writeRelaxScript materialises the emitted fragment plus a call against one
// Gemfile path, and returns the script to run. `trailer` is appended so a caller
// can prove execution continued past the call.
func writeRelaxScript(t *testing.T, dir, gemfile, trailer string) string {
	t.Helper()

	script := filepath.Join(dir, "relax.sh")
	body := "#!" + bashPath(t) + "\nset -eu\n" +
		support.GemfileConstraintScript() +
		"autoupdate_relax_gemfile_constraints " + shellQuote(gemfile) + "\n" + trailer
	require.NoError(t, os.WriteFile(script, []byte(body), 0o755))

	return script
}

// runGemfileRelax writes the given Gemfile, runs the emitted bash against it and
// returns the rewritten content.
func runGemfileRelax(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	gemfile := filepath.Join(dir, "Gemfile")
	require.NoError(t, os.WriteFile(gemfile, []byte(content), 0o600))

	out, err := exec.Command("bash", writeRelaxScript(t, dir, gemfile, "")).CombinedOutput()
	require.NoError(t, err, "relax script failed: %s", out)

	rewritten, err := os.ReadFile(gemfile)
	require.NoError(t, err)

	return string(rewritten)
}

func TestGemfileConstraintScript(t *testing.T) {
	t.Parallel()

	// Only `~>` moves, and only to `>=`: that operator exists for the implicit
	// ceiling it adds, so converting it keeps the floor the repository declared
	// and drops the cap. Everything else is a statement the repository made on
	// purpose -- an exact pin is the same kind of thing as a `.ruby-version`
	// file, and an explicit `<` is a refusal in as many words -- so the rows
	// that must *not* change carry as much weight here as the ones that must.
	cases := []struct{ name, given, want string }{
		{"single-quoted", "gem 'rails', '~> 6.0'\n", "gem 'rails', '>= 6.0'\n"},
		{"double-quoted", "gem \"rails\", \"~> 6.0.1\"\n", "gem \"rails\", \">= 6.0.1\"\n"},
		{"no space after operator", "gem 'puma', '~>5.6'\n", "gem 'puma', '>= 5.6'\n"},
		{"pre-release suffix kept", "gem 'r', '~> 7.1.0.beta1'\n", "gem 'r', '>= 7.1.0.beta1'\n"},
		{"exact pin untouched", "gem 'rails', '6.0.1'\n", "gem 'rails', '6.0.1'\n"},
		{"explicit ceiling untouched", "gem 'rails', '< 7'\n", "gem 'rails', '< 7'\n"},
		{"already open untouched", "gem 'rails', '>= 6.0'\n", "gem 'rails', '>= 6.0'\n"},
		{"unconstrained untouched", "gem 'rails'\n", "gem 'rails'\n"},
		{
			"compound keeps its ceiling",
			"gem 'rails', '~> 6.0', '< 7'\n", "gem 'rails', '>= 6.0', '< 7'\n",
		},
		{"prose is not a constraint", "# use ~> here\ngem 'r'\n", "# use ~> here\ngem 'r'\n"},
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
		script := writeRelaxScript(
			t, dir, filepath.Join(dir, "Gemfile"), "echo reached-the-end\n",
		)

		// when
		out, err := exec.Command("bash", script).CombinedOutput()

		// then
		require.NoError(t, err, "output: %s", out)
		assert.Contains(t, string(out), "reached-the-end")
	})
}
