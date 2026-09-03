package support

// GemfileConstraintScript is the bash fragment that lets bundler resolve past a
// major without leaving the Gemfile unbounded afterwards.
//
// Bundler has no flag for this. `bundle update` resolves inside whatever the
// Gemfile declares, and `--major` only lifts bundler's own ceiling, not the
// repository's -- so `gem "rails", "~> 6.0"` stays on 6.x for ever however
// `allow_major_updates` is set. Every other ecosystem has a native answer
// (`pub upgrade --major-versions`, `pdm --unconstrained`, `npm-check-updates`);
// Ruby is the one where the manifest has to be edited directly.
//
// It defines three functions, run around the resolution:
//
//   - autoupdate_relax_gemfile_constraints keeps a copy of the manifest and
//     rewrites every pessimistic constraint on a `gem` line -- `~> X.Y[.Z]` --
//     to `>= X.Y[.Z]`, so the resolution that follows may cross a major;
//   - autoupdate_retighten_gemfile_constraints rewrites the manifest from that
//     copy once the resolution succeeded: a constraint whose gem resolved past
//     the bound it expressed becomes `~>` the resolved version at the precision
//     the repository wrote, so `~> 6.0` becomes `~> 7.1` rather than staying
//     `>= 6.0`, while a constraint whose gem stayed inside its bound, or whose
//     gem the lockfile does not list, is put back untouched;
//   - autoupdate_restore_gemfile_constraints puts the copy back when the
//     resolution failed, so a widening that resolved nothing never ships as a
//     dropped ceiling beside a lockfile that ignores it.
//
// The widening is a means, not the result. Bundler needs the bound out of the
// way to resolve past it, but what the repository keeps is a raised bound,
// which is the contract `pub upgrade --major-versions` and `npm-check-updates`
// honour too: `^1.2.3` becomes `^2.0.0`, and the operator survives the bump. A
// `>=` left behind would be permanent -- turning `allow_major_updates` off
// again would restore nothing, and every later resolution, by any tool, would
// be free to take the next major with nothing in the Gemfile to stop it.
//
// Only `gem` declarations are rewritten, and anchoring on the call rather than
// on the operator is what keeps everything else out of reach by construction:
// the `ruby "~> 3.2"` directive is a statement about the interpreter, the same
// kind of thing as a `.ruby-version` file; a commented-out declaration is
// inert; a bundler `plugin` is not a dependency of the application. A
// declaration continued over several lines keeps a constraint carried onto a
// continuation line, which is the conservative outcome and a shape a Gemfile
// almost never has. Within a `gem` line nothing but `~>` moves: an exact pin
// (`"6.0.1"`) is a deliberate pin, an explicit ceiling (`"< 7"`, `"<= 6.9"`)
// is the repository saying no in as many words, and `>=`/`>` are already open.
//
// Only the Gemfile is rewritten. A `.gemspec` declares what *consumers* of the
// library must tolerate, which is a different statement from what this
// application may resolve to, and widening it would loosen someone else's
// constraints rather than this repository's.
//
// The result is deliberately visible in the diff: a reviewer sees `~> 6.0`
// become `~> 7.1` next to the `Gemfile.lock` change.
func GemfileConstraintScript() string {
	return gemfileRelaxScript() + gemfileRetightenScript() + gemfileRestoreScript()
}

// gemfileRelaxScript emits the widening, which also keeps the copy the other
// two functions work from. The sed is addressed to gem lines only: both quote
// styles, an optional space after the operator, and a version class kept
// narrow -- digits, dots, and the letters a pre-release suffix uses -- so a
// line that merely contains the operator in prose is not rewritten.
func gemfileRelaxScript() string {
	return `# autoupdate_relax_gemfile_constraints <gemfile>
# Keeps a copy of the manifest, then rewrites "~> X.Y[.Z]" to ">= X.Y[.Z]" on
# every gem line so bundler may resolve past a major. Exact pins, explicit
# upper bounds and already-open constraints are left alone, and so is every
# line that is not a gem declaration: the ruby directive, commented-out
# declarations, bundler plugins and prose.
autoupdate_relax_gemfile_constraints() {
    gemfile="$1"
    [ -f "$gemfile" ] || return 0

    # Outside the repository, so a run that stops early cannot commit it.
    autoupdate_gemfile_orig="$(mktemp)"
    cp "$gemfile" "$autoupdate_gemfile_orig"

    if ! sed -E "/^[[:space:]]*gem[[:space:](]/ { \
            s/'~>[[:space:]]*([0-9][0-9A-Za-z._-]*)'/'>= \1'/g; \
            s/\"~>[[:space:]]*([0-9][0-9A-Za-z._-]*)\"/\">= \1\"/g; \
        }" "$gemfile" > "$gemfile.tmp"; then
        rm -f "$gemfile.tmp"
        return 1
    fi
    mv "$gemfile.tmp" "$gemfile"

    if ! cmp -s "$autoupdate_gemfile_orig" "$gemfile"; then
        echo "  relaxed pessimistic constraints in $gemfile so majors can resolve"
    fi
}

`
}

// gemfileRetightenScript emits the rewrite that turns the widening back into a
// bound. It reads the resolved versions from the lockfile's specs -- the
// four-space-indented "name (version)" lines under every GEM, GIT and PATH
// block, with any platform suffix dropped -- and rewrites the *original*
// manifest rather than the widened one, so a ">=" the repository had written
// itself is never mistaken for one the widening produced. A bound moves only
// when the resolved version left it: "~> 6.0" fixes the major and "~> 6.0.1"
// fixes major and minor, so the comparison is on that many leading segments,
// and the replacement carries as many segments as the original wrote.
func gemfileRetightenScript() string {
	return `# autoupdate_retighten_gemfile_constraints <gemfile> <lockfile>
# After a successful resolution, rewrites the manifest from the copy taken
# before widening: a "~> X.Y[.Z]" whose gem resolved past the bound it
# expressed becomes "~>" the resolved version at the same precision, so
# "~> 6.0" moves to "~> 7.1" rather than staying ">= 6.0" for ever. A
# constraint whose gem stayed inside its bound, or whose gem the lockfile does
# not list, is put back exactly as it was.
autoupdate_retighten_gemfile_constraints() {
    gemfile="$1"
    lockfile="$2"
    [ -n "${autoupdate_gemfile_orig:-}" ] && [ -f "$autoupdate_gemfile_orig" ] || return 0

    if ! awk -v lock="$lockfile" -v sq="'" -v dq='"' '
        BEGIN {
            while ((getline line < lock) > 0) {
                if (line ~ /^    [^ ]+ \([^ )]+\)/) {
                    split(line, field, " ")
                    version = field[2]
                    gsub(/[()]/, "", version)
                    sub(/-.*$/, "", version)
                    if (!(field[1] in resolved)) resolved[field[1]] = version
                }
            }
            close(lock)
            quote = "[" sq dq "]"
            gemcall = "^[ \t]*gem[ \t(]+" quote "[^" sq dq "]+" quote
            pessimistic = quote "~>[ \t]*[0-9][0-9A-Za-z._-]*" quote
        }
        # prefix returns the first n dotted segments of a version.
        function prefix(version, n,    parts, count, i, out) {
            count = split(version, parts, "[.]")
            if (n > count) n = count
            out = parts[1]
            for (i = 2; i <= n; i++) out = out "." parts[i]
            return out
        }
        # bound returns the constraint to write for a gem that declared "~> ver":
        # ver itself when the gem is not in the lock or resolved inside the
        # bound, else the resolved version cut to the same number of segments.
        function bound(name, ver,    segments, fixed, parts) {
            if (!(name in resolved)) return ver
            segments = split(ver, parts, "[.]")
            fixed = segments - 1
            if (fixed < 1) fixed = 1
            if (prefix(resolved[name], fixed) == prefix(ver, fixed)) return ver
            return prefix(resolved[name], segments)
        }
        {
            line = $0
            if (match(line, gemcall)) {
                name = substr(line, RSTART, RLENGTH)
                sub("^[^" sq dq "]*" quote, "", name)
                sub(quote "$", "", name)
                out = ""
                rest = line
                while (match(rest, pessimistic)) {
                    found = substr(rest, RSTART, RLENGTH)
                    q = substr(found, 1, 1)
                    ver = substr(found, 2, length(found) - 2)
                    sub(/^~>[ \t]*/, "", ver)
                    raised = bound(name, ver)
                    if (raised != ver) found = q "~> " raised q
                    out = out substr(rest, 1, RSTART - 1) found
                    rest = substr(rest, RSTART + RLENGTH)
                }
                line = out rest
            }
            print line
        }
    ' "$autoupdate_gemfile_orig" > "$gemfile.tmp"; then
        rm -f "$gemfile.tmp"
        return 1
    fi
    mv "$gemfile.tmp" "$gemfile"

    if ! cmp -s "$autoupdate_gemfile_orig" "$gemfile"; then
        echo "  raised pessimistic constraints in $gemfile past the majors bundler resolved"
    fi
    rm -f "$autoupdate_gemfile_orig"
    autoupdate_gemfile_orig=""
}

`
}

// gemfileRestoreScript emits the fallback for a resolution that failed.
func gemfileRestoreScript() string {
	return `# autoupdate_restore_gemfile_constraints <gemfile>
# Puts the manifest back exactly as the repository declared it, for a
# resolution that failed: without this the widening would ship as a dropped
# ceiling beside a lockfile that ignores it, and a frozen install would then
# refuse the mismatch.
autoupdate_restore_gemfile_constraints() {
    gemfile="$1"
    [ -n "${autoupdate_gemfile_orig:-}" ] && [ -f "$autoupdate_gemfile_orig" ] || return 0

    cp "$autoupdate_gemfile_orig" "$gemfile"
    rm -f "$autoupdate_gemfile_orig"
    autoupdate_gemfile_orig=""
    echo "  restored the constraints in $gemfile: bundler could not resolve past them"
}

`
}
