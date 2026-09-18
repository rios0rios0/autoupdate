package support

// GoMajorAvailableScript is the bash fragment that reports a newer major of a
// direct requirement, published where `go get -u` cannot see it.
//
// Go's semantic import versioning puts v2 and above behind a `/vN` path
// suffix, which makes them a *different module*. `go get -u` upgrades the
// modules in the build list; it never reaches for a path nothing requires, so
// a dependency can sit two majors behind for years and every run of this tool
// will report the repository as up to date. Nothing else here closes that gap:
// GoMajorGuardScript exists to hold back the one boundary `-u` does cross
// (v0 to v1, which share the unsuffixed path), and GoCompileGuardScript only
// judges what an upgrade already did.
//
// So this reports rather than upgrades, and that is not a half measure.
// Crossing a major means changing every import of the module and then fixing
// whatever the new API renamed -- a migration someone decides to make, not a
// diff a bot produces unattended. What a bot can do is make sure nobody has to
// notice on their own.
//
// It defines three functions:
//
//   - autoupdate_go_direct_versions, which prints "path version" for every
//     requirement NOT marked `// indirect`;
//   - autoupdate_go_next_major_path, which names the module path the next
//     major would live at;
//   - autoupdate_go_report_new_majors, which probes for those and echoes a
//     GO_MAJOR_AVAILABLE marker per hit.
//
// Only direct requirements are probed. An indirect one is a transitive detail
// nobody chose, and whichever major of it the graph needs is the graph's
// business; reporting those would bury the handful of lines that are somebody's
// decision under a list nobody can act on.
func GoMajorAvailableScript() string {
	return goDirectReaderScript() + goNextMajorPathScript() + goReportNewMajorsScript()
}

// goDirectReaderScript emits the reader for the half of the graph a repository
// actually chose.
func goDirectReaderScript() string {
	return `# autoupdate_go_direct_versions <go.mod path>
# Prints "path version" for every requirement WITHOUT a trailing "// indirect"
# marker, covering both the parenthesised require block and the single-line
# "require path version" form.
autoupdate_go_direct_versions() {
    awk '
        /^require[ \t]*\(/ { inblock = 1; next }
        inblock && /^\)/    { inblock = 0; next }
        {
            line = $0
            if (line ~ /\/\/[ \t]*indirect[ \t]*$/) next
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
`
}

// goNextMajorPathScript emits the path arithmetic the probe walks.
func goNextMajorPathScript() string {
	return `
# autoupdate_go_next_major_path <module path> <version>
# Prints the module path the next major would be published at, or nothing when
# the version does not say which major it is.
#
#   example.com/foo      v1.2.3  -> example.com/foo/v2
#   example.com/foo      v0.4.0  -> example.com/foo/v2
#   example.com/foo/v3   v3.1.0  -> example.com/foo/v4
#
# v0 and v1 share the unsuffixed path, so the next *suffixed* major after
# either is v2 -- which is also why "-u" crosses v0 to v1 and stops there.
# A "+incompatible" version names its major the same way, and the module that
# adopts proper versioning after it publishes the one above.
#
# Modules versioned the gopkg.in way ("gopkg.in/yaml.v3") encode the major with
# a dot rather than a path element, and are skipped rather than probed. Their
# next major is not "<path>/v4" and never will be, so the probe would be a
# registry round trip whose answer is known in advance -- once per such
# requirement, on every repository, on every run.
autoupdate_go_next_major_path() {
    case "$1" in
        *.v[0-9]|*.v[0-9][0-9]) return 0 ;;
    esac

    _nm_base="$(printf '%s' "$1" | sed 's|/v[0-9][0-9]*$||')"
    _nm_major="$(printf '%s' "$2" | sed -n 's/^v\([0-9][0-9]*\).*/\1/p')"
    [ -n "$_nm_major" ] || return 0

    if [ "$_nm_major" -lt 2 ]; then
        _nm_next=2
    else
        _nm_next=$((_nm_major + 1))
    fi

    printf '%s/v%s' "$_nm_base" "$_nm_next"
}
`
}

// goReportNewMajorsScript emits the probe itself.
func goReportNewMajorsScript() string {
	return `
# autoupdate_go_report_new_majors <go binary> <go.mod path> <module dir>
# Probes each direct requirement for a newer major and echoes one
# GO_MAJOR_AVAILABLE marker per hit, for the Go half to put in the pull request.
#
# Existence is decided on the VERSION LIST, not on the exit status.
# "go list -m -versions example.com/foo/v2" exits 0 for a major that was never
# published: it answers with the path and an empty version list. Reading the
# status instead would report every direct requirement as having a new major
# waiting, which is worse than not reporting at all -- a notice nobody can
# trust is one everybody learns to skip.
#
# The walk continues past a hit so a repository two majors behind is told about
# the newer one, and stops at the first miss. It is capped because the loop is
# driven by what a registry answers, and an unbounded remote-driven loop in an
# unattended run is a way to hang a nightly job.
#
# Nothing here writes to go.mod: "-versions" is a query, and the module being
# probed is deliberately not added to the build list.
autoupdate_go_report_new_majors() {
    go_binary="$1"
    go_mod_path="$2"
    module_dir="$3"
    direct_file="$(mktemp)"

    autoupdate_go_direct_versions "$go_mod_path" > "$direct_file"

    while read -r module_path current_version; do
        [ -n "$module_path" ] || continue

        probe_path="$(autoupdate_go_next_major_path "$module_path" "$current_version")"
        found_path=""
        found_version=""
        probes=0

        while [ -n "$probe_path" ] && [ "$probes" -lt 8 ]; do
            probes=$((probes + 1))
            probe_version="$("$go_binary" list -m -versions "$probe_path" 2>/dev/null |
                awk 'NF >= 2 { print $NF }')"
            [ -n "$probe_version" ] || break
            found_path="$probe_path"
            found_version="$probe_version"
            probe_path="$(autoupdate_go_next_major_path "$found_path" "$found_version")"
        done

        [ -n "$found_path" ] || continue

        echo "GO_MAJOR_AVAILABLE=${module_dir}|${module_path}|${current_version}|${found_path}|${found_version}"
        echo "  ${module_path} ${current_version}: a newer major is published as ${found_path} ${found_version}"
        echo "    not applied -- it is a different module path, so taking it means rewriting imports"
    done < "$direct_file"

    rm -f "$direct_file"
}
`
}
