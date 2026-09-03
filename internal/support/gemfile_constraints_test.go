package support_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/support"
)

// gemfileFixtureLock stands in for what `bundle update` writes against the
// *widened* manifest: the specs are what the re-tightening reads, and the rest
// is there so the parser has to step over the same shapes a real lockfile
// carries -- nested dependencies with their own constraints, a platform-suffixed
// version, and the DEPENDENCIES block. That block records the widened
// requirements bundler resolved against (`rails (>= 6.0)`), which is why it
// disagrees with the re-tightened Gemfile the rows below assert: the fragment
// never runs bundler, and reconciling the two is the caller's `bundle lock`,
// asserted in the Ruby updater's tests.
const gemfileFixtureLock = `GEM
  remote: https://rubygems.org/
  specs:
    actionpack (7.1.3)
      rack (~> 2.0)
    nokogiri (1.16.0-x86_64-linux)
      racc (~> 1.4)
    puma (6.4.2)
    racc (1.7.3)
    rack (2.2.8)
    rails (7.1.3)
      actionpack (= 7.1.3)

PLATFORMS
  x86_64-linux

DEPENDENCIES
  nokogiri (>= 1.14.0)
  puma (>= 5.6)
  rails (>= 6.0)

BUNDLED WITH
   2.5.6
`

// writeGemfileScript materialises the emitted fragment followed by calls, the
// bash lines exercising it, and returns the script path. It runs under
// `set -eu` because the surrounding upgrade script does, so a fragment that
// trips either option fails here rather than in a clone.
func writeGemfileScript(t *testing.T, dir, calls string) string {
	t.Helper()

	script := filepath.Join(dir, "gemfile.sh")
	body := "#!" + bashPath(t) + "\nset -eu\n" + support.GemfileConstraintScript() + calls
	require.NoError(t, os.WriteFile(script, []byte(body), 0o755))

	return script
}

// runGemfileScript runs the emitted fragment plus calls and returns the
// combined output, failing the test on a non-zero exit.
func runGemfileScript(t *testing.T, dir, calls string) string {
	t.Helper()

	out, err := exec.Command("bash", writeGemfileScript(t, dir, calls)).CombinedOutput()
	require.NoError(t, err, "gemfile script failed: %s", out)

	return string(out)
}

// gemfileHarness is one temporary directory holding a Gemfile and, when the
// case needs one, the lockfile the resolution is pretended to have written.
type gemfileHarness struct {
	dir, gemfile, lockfile string
}

func newGemfileHarness(t *testing.T, gemfile, lock string) gemfileHarness {
	t.Helper()

	h := gemfileHarness{dir: t.TempDir()}
	h.gemfile = filepath.Join(h.dir, "Gemfile")
	h.lockfile = filepath.Join(h.dir, "Gemfile.lock")
	require.NoError(t, os.WriteFile(h.gemfile, []byte(gemfile), 0o600))
	if lock != "" {
		require.NoError(t, os.WriteFile(h.lockfile, []byte(lock), 0o600))
	}

	return h
}

func (h gemfileHarness) relaxCall() string {
	return "autoupdate_relax_gemfile_constraints " + shellQuote(h.gemfile) + "\n"
}

func (h gemfileHarness) retightenCall() string {
	return "autoupdate_retighten_gemfile_constraints " +
		shellQuote(h.gemfile) + " " + shellQuote(h.lockfile) + "\n"
}

func (h gemfileHarness) restoreCall() string {
	return "autoupdate_restore_gemfile_constraints " + shellQuote(h.gemfile) + "\n"
}

func (h gemfileHarness) read(t *testing.T) string {
	t.Helper()

	content, err := os.ReadFile(h.gemfile)
	require.NoError(t, err)

	return string(content)
}

// entries lists what the directory holds afterwards, so a case can assert the
// fragment left no working file behind for `git add -A` to sweep up.
func (h gemfileHarness) entries(t *testing.T) []string {
	t.Helper()

	dirEntries, err := os.ReadDir(h.dir)
	require.NoError(t, err)

	names := make([]string, 0, len(dirEntries))
	for _, entry := range dirEntries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	return names
}

func TestGemfileConstraintScript(t *testing.T) {
	t.Parallel()

	// Only `~>` moves, only on gem lines, and only to `>=`: that operator
	// exists for the implicit ceiling it adds, so converting it keeps the
	// floor the repository declared and drops the cap. Everything else is a
	// statement the repository made on purpose -- an exact pin is the same
	// kind of thing as a `.ruby-version` file, an explicit `<` is a refusal in
	// as many words, and the `ruby` directive describes the interpreter -- so
	// the rows that must *not* change carry as much weight as the ones that
	// must.
	cases := []struct{ name, given, want string }{
		{"single-quoted", "gem 'rails', '~> 6.0'\n", "gem 'rails', '>= 6.0'\n"},
		{"double-quoted", "gem \"rails\", \"~> 6.0.1\"\n", "gem \"rails\", \">= 6.0.1\"\n"},
		{"no space after operator", "gem 'puma', '~>5.6'\n", "gem 'puma', '>= 5.6'\n"},
		{"pre-release suffix kept", "gem 'r', '~> 7.1.0.beta1'\n", "gem 'r', '>= 7.1.0.beta1'\n"},
		{"trailing options kept", "gem 'pg', '~> 1.5', require: false\n", "gem 'pg', '>= 1.5', require: false\n"},
		{"parenthesised call", "gem('puma', '~> 6.4')\n", "gem('puma', '>= 6.4')\n"},
		{
			"indented inside a group",
			"group :test do\n  gem 'rspec', '~> 3.12'\nend\n", "group :test do\n  gem 'rspec', '>= 3.12'\nend\n",
		},
		{"exact pin untouched", "gem 'rails', '6.0.1'\n", "gem 'rails', '6.0.1'\n"},
		{"explicit ceiling untouched", "gem 'rails', '< 7'\n", "gem 'rails', '< 7'\n"},
		{"already open untouched", "gem 'rails', '>= 6.0'\n", "gem 'rails', '>= 6.0'\n"},
		{"unconstrained untouched", "gem 'rails'\n", "gem 'rails'\n"},
		{
			"compound keeps its ceiling",
			"gem 'rails', '~> 6.0', '< 7'\n", "gem 'rails', '>= 6.0', '< 7'\n",
		},
		{"prose is not a constraint", "# use ~> here\ngem 'r'\n", "# use ~> here\ngem 'r'\n"},
		{"ruby directive untouched", "ruby '~> 3.2'\n", "ruby '~> 3.2'\n"},
		{"commented-out declaration untouched", "# gem 'rails', '~> 6.0'\n", "# gem 'rails', '~> 6.0'\n"},
		{"bundler plugin untouched", "plugin 'bundler-graph', '~> 0.2'\n", "plugin 'bundler-graph', '~> 0.2'\n"},
		// The anchor is the gem call, so a constraint carried onto a
		// continuation line keeps its ceiling -- the conservative outcome, and
		// a shape a Gemfile almost never has.
		{"continuation line left alone", "gem 'rails',\n    '~> 6.0'\n", "gem 'rails',\n    '~> 6.0'\n"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// given
			h := newGemfileHarness(t, testCase.given, "")

			// when
			runGemfileScript(t, h.dir, h.relaxCall())

			// then
			assert.Equal(t, testCase.want, h.read(t))
		})
	}

	t.Run("should tolerate a missing Gemfile", func(t *testing.T) {
		t.Parallel()

		// given -- a Ruby repository without a Gemfile is not an error, and the
		// surrounding script runs under `set -e`
		dir := t.TempDir()
		missing := filepath.Join(dir, "Gemfile")
		calls := "autoupdate_relax_gemfile_constraints " + shellQuote(missing) + "\n" +
			"autoupdate_retighten_gemfile_constraints " + shellQuote(missing) + " " +
			shellQuote(filepath.Join(dir, "Gemfile.lock")) + "\n" +
			"autoupdate_restore_gemfile_constraints " + shellQuote(missing) + "\n" +
			"echo reached-the-end\n"

		// when
		out := runGemfileScript(t, dir, calls)

		// then
		assert.Contains(t, out, "reached-the-end")
	})
}

func TestGemfileConstraintRetighten(t *testing.T) {
	t.Parallel()

	// Each row runs the whole cycle -- widen, then re-tighten against the
	// fixture lockfile standing in for what bundler wrote. A bound moves only
	// when the resolved version left it, and moves to the same precision the
	// repository wrote, so the manifest keeps stating a bound afterwards.
	cases := []struct{ name, given, want string }{
		{"raises a two-segment bound past the major", "gem 'rails', '~> 6.0'\n", "gem 'rails', '~> 7.1'\n"},
		{"keeps a two-segment bound the gem stayed inside", "gem 'rails', '~> 7.0'\n", "gem 'rails', '~> 7.0'\n"},
		{"raises a three-segment bound at its own precision", "gem 'rails', '~> 6.0.1'\n", "gem 'rails', '~> 7.1.3'\n"},
		{"keeps a three-segment bound the gem stayed inside", "gem 'rack', '~> 2.2.1'\n", "gem 'rack', '~> 2.2.1'\n"},
		{"raises a one-segment bound", "gem 'rails', '~> 6'\n", "gem 'rails', '~> 7'\n"},
		{"drops the platform suffix", "gem 'nokogiri', '~> 1.14.0'\n", "gem 'nokogiri', '~> 1.16.0'\n"},
		{"double-quoted", "gem \"rails\", \"~> 6.0\"\n", "gem \"rails\", \"~> 7.1\"\n"},
		{"puts back a gem the lock does not list", "gem 'missing', '~> 1.0'\n", "gem 'missing', '~> 1.0'\n"},
		{"exact pin untouched", "gem 'rails', '6.0.1'\n", "gem 'rails', '6.0.1'\n"},
		{"already open untouched", "gem 'rails', '>= 6.0'\n", "gem 'rails', '>= 6.0'\n"},
		{"normalises the spacing only when it moves", "gem 'puma', '~>5.6'\n", "gem 'puma', '~> 6.4'\n"},
		{"keeps the spacing when it stays", "gem 'puma', '~>6.0'\n", "gem 'puma', '~>6.0'\n"},
		{"compound keeps its ceiling and its floor", "gem 'rack', '~> 2.0', '< 3'\n", "gem 'rack', '~> 2.0', '< 3'\n"},
		{"parenthesised call", "gem('rails', '~> 6.0')\n", "gem('rails', '~> 7.1')\n"},
		{
			"indented inside a group",
			"group :test do\n  gem 'rails', '~> 6.0'\nend\n", "group :test do\n  gem 'rails', '~> 7.1'\nend\n",
		},
		{
			"ruby directive survives the cycle",
			"ruby '~> 3.1'\ngem 'rails', '~> 6.0'\n", "ruby '~> 3.1'\ngem 'rails', '~> 7.1'\n",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// given
			h := newGemfileHarness(t, testCase.given, gemfileFixtureLock)

			// when
			runGemfileScript(t, h.dir, h.relaxCall()+h.retightenCall())

			// then
			assert.Equal(t, testCase.want, h.read(t))
		})
	}

	t.Run("should leave no working file behind", func(t *testing.T) {
		t.Parallel()

		// given -- the clone is swept with `git add -A` afterwards, so a
		// leftover copy or temp file would be committed
		h := newGemfileHarness(t, "gem 'rails', '~> 6.0'\n", gemfileFixtureLock)

		// when
		out := runGemfileScript(t, h.dir, h.relaxCall()+h.retightenCall())

		// then
		assert.Equal(t, []string{"Gemfile", "Gemfile.lock", "gemfile.sh"}, h.entries(t))
		assert.Contains(t, out, "relaxed pessimistic constraints")
		assert.Contains(t, out, "raised pessimistic constraints")
	})

	t.Run("should be a no-op without a prior widening", func(t *testing.T) {
		t.Parallel()

		// given
		h := newGemfileHarness(t, "gem 'rails', '~> 6.0'\n", gemfileFixtureLock)

		// when
		out := runGemfileScript(t, h.dir, h.retightenCall()+"echo reached-the-end\n")

		// then
		assert.Equal(t, "gem 'rails', '~> 6.0'\n", h.read(t))
		assert.Contains(t, out, "reached-the-end")
	})
}

func TestGemfileConstraintRestore(t *testing.T) {
	t.Parallel()

	t.Run("should put the manifest back when the resolution failed", func(t *testing.T) {
		t.Parallel()

		// given -- a widening whose resolution failed must not ship as a
		// dropped ceiling beside a lockfile that ignores it
		original := "ruby '~> 3.1'\ngem 'rails', '~> 6.0'\ngem 'rack', '~>2.0', '< 3'\n"
		h := newGemfileHarness(t, original, "")

		// when
		out := runGemfileScript(t, h.dir, h.relaxCall()+h.restoreCall())

		// then
		assert.Equal(t, original, h.read(t))
		assert.Equal(t, []string{"Gemfile", "gemfile.sh"}, h.entries(t))
		assert.Contains(t, out, "restored the constraints")
	})

	t.Run("should be a no-op without a prior widening", func(t *testing.T) {
		t.Parallel()

		// given
		h := newGemfileHarness(t, "gem 'rails', '~> 6.0'\n", "")

		// when
		out := runGemfileScript(t, h.dir, h.restoreCall()+"echo reached-the-end\n")

		// then
		assert.Equal(t, "gem 'rails', '~> 6.0'\n", h.read(t))
		assert.Contains(t, out, "reached-the-end")
	})
}
