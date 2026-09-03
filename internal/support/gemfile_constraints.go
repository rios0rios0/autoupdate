package support

// GemfileConstraintScript is the bash fragment that relaxes the pessimistic
// version constraints in a Gemfile so bundler may resolve past a major.
//
// Bundler has no flag for this. `bundle update` resolves inside whatever the
// Gemfile declares, and `--major` only lifts bundler's own ceiling, not the
// repository's -- so `gem "rails", "~> 6.0"` stays on 6.x for ever however
// `allow_major_updates` is set. Every other ecosystem has a native answer
// (`pub upgrade --major-versions`, `pdm --unconstrained`, `npm-check-updates`);
// Ruby is the one where the manifest has to be edited directly.
//
// It rewrites exactly one thing: `~> X.Y[.Z]` becomes `>= X.Y[.Z]`. The
// pessimistic operator's whole purpose is the implicit ceiling it adds, so
// converting it to `>=` removes that ceiling while keeping the floor the
// repository actually declared -- the minimum version its code needs. Nothing
// else is touched:
//
//   - an exact pin (`gem "rails", "6.0.1"`) is a deliberate pin, the same kind
//     of statement as a `.ruby-version` file, and is left alone;
//   - an explicit ceiling (`"< 7"`, `"<= 6.9"`) is the repository saying no in
//     as many words, and is left alone;
//   - `>=` and `>` are already open and need nothing.
//
// Only the Gemfile is rewritten. A `.gemspec` declares what *consumers* of the
// library must tolerate, which is a different statement from what this
// application may resolve to, and widening it would loosen someone else's
// constraints rather than this repository's.
//
// The rewrite is deliberately visible in the diff. A reviewer sees the
// constraint change next to the `Gemfile.lock` change, which is the same
// contract `pub upgrade --major-versions` offers for `pubspec.yaml`.
func GemfileConstraintScript() string {
	return `# autoupdate_relax_gemfile_constraints <file>
# Rewrites "~> X.Y[.Z]" to ">= X.Y[.Z]" so bundler may resolve past a major.
# Leaves exact pins, explicit upper bounds and already-open constraints alone.
autoupdate_relax_gemfile_constraints() {
    gemfile="$1"
    [ -f "$gemfile" ] || return 0

    # Both quote styles, and an optional space after the operator. The version
    # class stays narrow -- digits, dots, and the letters a pre-release suffix
    # uses -- so a line that merely contains "~>" in prose is not rewritten.
    sed -E "s/'~>[[:space:]]*([0-9][0-9A-Za-z._-]*)'/'>= \1'/g; \
            s/\"~>[[:space:]]*([0-9][0-9A-Za-z._-]*)\"/\">= \1\"/g" \
        "$gemfile" > "$gemfile.tmp" && mv "$gemfile.tmp" "$gemfile"

    if ! git diff --quiet -- "$gemfile" 2>/dev/null; then
        echo "  relaxed pessimistic constraints in $gemfile so majors can resolve"
    fi
}

`
}
