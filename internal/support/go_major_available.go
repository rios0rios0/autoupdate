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
// It defines seven functions:
//
//   - autoupdate_go_direct_versions, which prints "path version" for every
//     requirement NOT marked `// indirect`;
//   - autoupdate_go_next_major_path, which names the module path the next
//     major would live at;
//   - autoupdate_go_path_major, which names the major a module path ends in;
//   - autoupdate_go_newest_release, which names the newest finished release
//     published at a module path;
//   - autoupdate_go_module_identity, which names the module path a published
//     version declares in its own go.mod, and the repository it came from;
//   - autoupdate_go_same_repository, which compares two of those
//     repositories;
//   - autoupdate_go_report_new_majors, which probes for those and echoes a
//     GO_MAJOR_AVAILABLE marker per hit.
//
// Only direct requirements are probed. An indirect one is a transitive detail
// nobody chose, and whichever major of it the graph needs is the graph's
// business; reporting those would bury the handful of lines that are somebody's
// decision under a list nobody can act on.
func GoMajorAvailableScript() string {
	return goDirectReaderScript() + goNextMajorPathScript() + goModuleQueriesScript() +
		goReportNewMajorsDoc() + goReportNewMajorsScript()
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

// goModuleQueriesScript emits the two registry queries the probe is built
// from. Both are read-only: neither adds the module to the build list.
func goModuleQueriesScript() string {
	return `
# Both queries pass "-mod=readonly". In a module that vendors its dependencies
# the go command defaults to "-mod=vendor", where neither query is allowed at
# all ("can't determine available versions using the vendor directory"); with
# its errors discarded, the probe used to read that as "no newer major" for
# every vendored module. Read-only is also the default without a vendor
# directory, so nothing changes for the other modules.

# autoupdate_go_newest_release <go binary> <module path>
# Prints the newest FINISHED release published at the path, or nothing.
#
# "-versions" lists pre-releases too, in semver order among the releases. A
# version is read the way semver and "go get -u" read it: a "-" after the patch
# number makes it unfinished whatever word follows, and build metadata
# ("+incompatible") is no part of that.
autoupdate_go_newest_release() {
    "$1" list -mod=readonly -m -versions "$2" 2>/dev/null |
        awk '{
            for (i = NF; i >= 2; i--) {
                release = $i
                sub(/\+.*$/, "", release)
                if (index(release, "-") == 0) { print $i; exit }
            }
        }'
}

# autoupdate_go_module_identity <go binary> <module path> <version>
# Prints "<declared path>|<repository URL>" for a published version: the module
# path on the "module" line of its own go.mod, and the repository the registry
# fetched it from (empty when the registry does not say). Prints "!" when the
# go command refuses the version at that path, and nothing when it cannot tell.
#
# A registry lists a repository's tags under every path that leads to the
# repository, including a path the module stopped answering to: charmbracelet's
# v2 modules are tagged in the same GitHub repositories as v1, so
# "github.com/charmbracelet/bubbletea/v2" lists v2.1.1, but its go.mod declares
# "charm.land/bubbletea/v2" and "go get" refuses the GitHub path outright. The
# go command fetches that go.mod for this query without judging it, so the
# mismatch can be read rather than failed on.
#
# What it does refuse is a go.mod on the wrong major -- a v2 tag whose go.mod
# forgot the /v2 (hashicorp/consul), or no go.mod at all under a /vN path
# (docker/docker/v28) -- and it says so as an "invalid version". Nothing can
# require that version at that path, which is the "!".
#
# The go.mod is read the way the go command reads it: a path may be quoted, and
# a line may end in "\r".
autoupdate_go_module_identity() {
    _mi_err="$(mktemp)"
    if _mi_out="$("$1" list -mod=readonly -m -f '{{.GoMod}}|{{with .Origin}}{{.URL}}{{end}}' \
        "$2@$3" 2>"$_mi_err")"; then
        rm -f "$_mi_err"
        _mi_mod="${_mi_out%%|*}"
        if [ -z "$_mi_mod" ] || [ ! -f "$_mi_mod" ]; then
            return 0
        fi
        _mi_declared="$(awk '
            { sub(/\r$/, "") }
            $1 == "module" && NF >= 2 && $2 != "(" { gsub(/["` + "`" + `]/, "", $2); print $2; exit }
        ' "$_mi_mod" 2>/dev/null || true)"
        if [ -n "$_mi_declared" ]; then
            printf '%s|%s\n' "$_mi_declared" "${_mi_out#*|}"
        fi
        return 0
    fi

    if grep -q 'invalid version' "$_mi_err" 2>/dev/null; then
        printf '!\n'
    fi
    rm -f "$_mi_err"
}

# autoupdate_go_path_major <module path>
# Prints the major a module path ends in -- "2" for both "example.com/foo/v2"
# and the gopkg.in spelling "gopkg.in/foo.v2" -- or nothing when it ends in
# neither.
autoupdate_go_path_major() {
    printf '%s' "$1" | sed -n 's|.*[/.]v\([0-9][0-9]*\)$|\1|p'
}

# autoupdate_go_same_repository <repository URL> <repository URL>
# Succeeds when both URLs are known and name the same repository.
autoupdate_go_same_repository() {
    [ -n "$1" ] && [ -n "$2" ] || return 1
    _sr_first="$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed 's|/*$||; s|\.git$||')"
    _sr_second="$(printf '%s' "$2" | tr '[:upper:]' '[:lower:]' | sed 's|/*$||; s|\.git$||')"
    [ "$_sr_first" = "$_sr_second" ]
}
`
}

// goReportNewMajorsDoc emits the comment the probe is introduced by in the
// generated script, which is where a reader of a run's script looks for it.
func goReportNewMajorsDoc() string {
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
# Only a FINISHED release on that list counts. Reading its last entry offered
# go-git's /v6 -- published as v6.0.0-beta.1 and nothing else -- as a major to
# migrate to, although a pre-release is never a candidate anywhere in this
# tool. The newest finished release is the one reported, so a major that went
# on to publish v2.1.0-rc.1 is still offered at v2.0.0, and a major published
# only as pre-releases is a miss.
#
# A hit is only a hit at the path its go.mod declares. A version the go command
# refuses at its path is a miss: nothing can require it there. When the release
# found declares another path for the same major, the probe moves to that path
# and asks again there -- its own versions, its own go.mod -- and the walk
# carries on from it, because the next major will be published where this one
# moved to. The move is only taken when the registry names the repository each
# side came from and it is the same one: a vanity path for the same repository
# (charm.land and github.com/charmbracelet both lead to charmbracelet's) is a
# move, while a fork still carrying the tags of the repository it was forked
# from would otherwise be pointed back at that repository. A move nobody can
# vouch for, or onto another major, is a miss. A go.mod that cannot be read at
# all keeps the path that was probed: a registry hiccup must not hide a notice
# the old probe would have given.
#
# The walk continues past a hit so a repository two majors behind is told about
# the newer one, and stops at the first miss. It is capped because the loop is
# driven by what a registry answers, and an unbounded remote-driven loop in an
# unattended run is a way to hang a nightly job. The cap is there to stop a
# registry that never stops answering, not to decide how far behind a
# dependency may be: at 8 it did the second, and go-github -- which publishes
# more than a major a month -- was offered at v74 to a repository on v66 while
# v93 was current, so the migration it prepared was nineteen majors behind
# before anyone opened it. 100 majors is years of that pace, and each probe is
# two sub-second queries, made only while the registry keeps answering. A move
# to a declared path is a probe like any other, so a chain of modules pointing
# at each other cannot outrun the cap either.
#
# Nothing here writes to go.mod: both are queries, and the module being probed
# is deliberately not added to the build list.
`
}

// goReportNewMajorsScript emits the probe itself.
func goReportNewMajorsScript() string {
	return `autoupdate_go_report_new_majors() {
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
        moved_from=""
        probes=0

        while [ -n "$probe_path" ] && [ "$probes" -lt 100 ]; do
            probes=$((probes + 1))
            probe_version="$(autoupdate_go_newest_release "$go_binary" "$probe_path")"
            [ -n "$probe_version" ] || break

            identity="$(autoupdate_go_module_identity "$go_binary" "$probe_path" "$probe_version")"
            [ "$identity" != "!" ] || break
            declared_path="${identity%%|*}"
            repository="${identity#*|}"

            # Arrived by a move: only the repository the probe moved away from
            # may publish here, or this is somebody else's module.
            if [ -n "$moved_from" ]; then
                autoupdate_go_same_repository "$moved_from" "$repository" || break
                moved_from=""
            fi

            if [ -n "$declared_path" ] && [ "$declared_path" != "$probe_path" ]; then
                [ -n "$repository" ] || break
                [ "$(autoupdate_go_path_major "$declared_path")" = \
                    "$(autoupdate_go_path_major "$probe_path")" ] || break
                moved_from="$repository"
                probe_path="$declared_path"
                continue
            fi

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
