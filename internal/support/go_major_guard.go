package support

// GoMajorGuardScript is the bash fragment that holds a Go dependency at its
// current major version across `go get -u`.
//
// `go get -u` is documented as upgrading to the latest *minor or patch*
// release, and for most modules it does: Go's semantic import versioning puts
// v2 and above behind a `/v2` path suffix, so a major upgrade is a different
// module path and `-u` will never reach for it on its own.
//
// The exception is the boundary below that suffix. v0 and v1 share the
// unsuffixed path, so a module that tags v1.0.0 after years on v0.x becomes,
// to MVS, simply the highest version of the same module — and `-u` takes it.
// `+incompatible` modules cross the same way, for the same reason. Go's own
// convention says v0 offers no compatibility promise, which is exactly why the
// v0-to-v1 jump is the one most likely to break a build.
//
// It broke ours: `gobwas/glob` v1.0.0 removed the `Glob` interface in favour of
// a concrete `*Pattern`, and every published `gocolly/colly` release still
// declares `compiledGlob glob.Glob`. An indirect dependency nobody had named
// crossed a major boundary and the module stopped compiling — inside a pull
// request whose title claimed a routine dependency update.
//
// The guard compares go.mod before and after, and puts back anything whose
// major moved. Everything else in the upgrade is kept, so the pull request
// still carries every safe update rather than being abandoned wholesale.
//
// It defines four functions:
//
//   - autoupdate_go_module_versions, which prints "path version" for every
//     requirement in a go.mod;
//   - autoupdate_go_major_of, which extracts the major from a version;
//   - autoupdate_go_hold_major_jumps, which reverts the crossings and reports
//     each one;
//   - autoupdate_go_report_unheld, which re-reads go.mod afterwards and names
//     anything the hold failed to fix or made worse.
//
// It calls autoupdate_version_is_newer, so a caller must emit
// VersionGuardScript() as well -- which the Go updater already does.
func GoMajorGuardScript() string {
	return `# autoupdate_go_module_versions <go.mod path>
# Prints "path version" for every requirement, covering both the parenthesised
# require block and single-line "require path version" forms. Trailing "// indirect"
# markers are stripped, because an indirect requirement crosses a major exactly
# the way a direct one does -- and is far likelier to, nobody having chosen it.
autoupdate_go_module_versions() {
    awk '
        /^require[ \t]*\(/ { inblock = 1; next }
        inblock && /^\)/    { inblock = 0; next }
        {
            line = $0
            sub(/\/\/.*$/, "", line)
            gsub(/^[ \t]+|[ \t]+$/, "", line)
            if (line == "") next
            if (!inblock) {
                if (line !~ /^require[ \t]/) next
                sub(/^require[ \t]+/, "", line)
            }
            n = split(line, f, /[ \t]+/)
            if (n >= 2 && f[2] ~ /^v[0-9]/) print f[1], f[2]
        }
    ' "$1" 2>/dev/null || true
}

# autoupdate_go_major_of <version>
# v0.2.3 -> 0 ; v1.0.0 -> 1 ; v2.0.0+incompatible -> 2
autoupdate_go_major_of() {
    printf '%s' "$1" | sed -n 's/^v\([0-9][0-9]*\).*/\1/p'
}

# autoupdate_go_hold_major_jumps <go binary> <before snapshot> <go.mod path>
# Puts back every requirement whose major version moved, leaving the rest of the
# upgrade in place. A dependency that cannot be held is reported and the loop
# continues, the same way the surrounding script treats a failed "go get".
#
# Returns the status of autoupdate_go_report_unheld, which it ends with: non-zero
# when a hold did not take or a requirement moved backwards. Callers must branch
# on that rather than invoke it bare -- the generated scripts run under "set -e",
# where a bare call would abort the whole run on the first module the guard found
# something on, losing the safe part of the upgrade it had already made.
autoupdate_go_hold_major_jumps() {
    go_binary="$1"
    before_file="$2"
    go_mod_path="$3"
    after_file="$(mktemp)"
    held=0

    autoupdate_go_module_versions "$go_mod_path" > "$after_file"

    while read -r module_path old_version; do
        [ -n "$module_path" ] || continue
        new_version="$(awk -v p="$module_path" '$1 == p { print $2; exit }' "$after_file")"
        [ -n "$new_version" ] || continue
        [ "$new_version" != "$old_version" ] || continue

        old_major="$(autoupdate_go_major_of "$old_version")"
        new_major="$(autoupdate_go_major_of "$new_version")"
        [ -n "$old_major" ] && [ -n "$new_major" ] || continue
        [ "$old_major" != "$new_major" ] || continue

        echo "  holding ${module_path} at ${old_version}: ${new_version} crosses a major version boundary"
        if ! "$go_binary" get "${module_path}@${old_version}" 2>&1; then
            echo "    WARNING: could not hold ${module_path} at ${old_version}"
        fi
        held=1
    done < "$before_file"

    rm -f "$after_file"

    if [ "$held" = "1" ]; then
        echo "  re-running go mod tidy after holding major version jumps..."
        "$go_binary" mod tidy 2>&1 || echo "    WARNING: go mod tidy had some errors"
    fi

    autoupdate_go_report_unheld "$before_file" "$go_mod_path"
}

# autoupdate_go_report_unheld <before snapshot> <go.mod path>
# Re-reads go.mod after everything above has run and says what actually landed.
#
# The hold is two commands away from the state it is judged on: "go get <old>"
# can fail because an upgraded direct dependency now requires the major being
# held -- the exact conflict the guard exists for -- and "go mod tidy" resolves
# the graph again afterwards. Deciding from the pre-tidy snapshot therefore
# answers for a go.mod that is not the one being committed, and both outcomes it
# misses are worse than the jump: a major that shipped under a pull request
# saying it was held, and a requirement dragged *below* where the run started,
# because "go get <path>@<older>" downgrades everything that required the higher
# version. That second one is the failure CLAUDE.md already names under "Version
# Pins Only Move Forwards", reached by a new route.
#
# This reports rather than mutates. Restoring would be another "go get", which
# cascades the same way and could bury the problem a level deeper; naming it
# leaves a reviewer something to act on. Returns non-zero when anything is
# unresolved, so the caller can keep its claims honest.
autoupdate_go_report_unheld() {
    before_file="$1"
    go_mod_path="$2"
    final_file="$(mktemp)"
    unresolved=0

    autoupdate_go_module_versions "$go_mod_path" > "$final_file"

    while read -r module_path old_version; do
        [ -n "$module_path" ] || continue
        final_version="$(awk -v p="$module_path" '$1 == p { print $2; exit }' "$final_file")"
        [ -n "$final_version" ] || continue
        [ "$final_version" != "$old_version" ] || continue

        old_major="$(autoupdate_go_major_of "$old_version")"
        final_major="$(autoupdate_go_major_of "$final_version")"

        if [ -n "$old_major" ] && [ -n "$final_major" ] && [ "$old_major" != "$final_major" ]; then
            echo "  WARNING: ${module_path} is still ${final_version}: the hold at ${old_version} did not take"
            unresolved=1
            continue
        fi

        # Same major but lower than where the run started: a cascade from some
        # other hold, and a downgrade inside a branch titled as an upgrade.
        if ! autoupdate_version_is_newer "$final_version" "$old_version"; then
            echo "  WARNING: ${module_path} moved backwards, ${old_version} -> ${final_version}"
            unresolved=1
        fi
    done < "$before_file"

    rm -f "$final_file"

    return "$unresolved"
}

`
}
