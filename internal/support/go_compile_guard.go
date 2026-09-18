package support

// GoCompileGuardScript is the bash fragment that keeps a Go upgrade from
// shipping a module that no longer compiles.
//
// GoMajorGuardScript covers the case where an indirect dependency crosses a
// major version boundary. This is its sibling and its blind spot: an indirect
// dependency that breaks the build *without* moving a major at all.
//
// `go get -u` raises indirect requirements past what anything in the graph
// actually asks for, and an indirect requirement is the one nobody chose and
// nobody reads. When the module it belongs to is one half of a pair that must
// move together, raising one half alone is enough. It happened to a set of
// Kubernetes test harnesses: `k8s.io/kube-openapi` is untagged, so every
// upgrade of it is a pseudo-version -- same major, same minor, nothing for a
// version comparison to catch -- and the revision `-u` reached for had moved
// `schemaconv` onto `sigs.k8s.io/structured-merge-diff/v7` while the
// `k8s.io/apimachinery` release every one of those modules requires still
// builds its `managedfields` on v6. The two stopped type-checking together.
// Minimal version selection would have picked the compatible revision on its
// own; the explicit raise is what broke it.
//
// The guard verifies rather than predicts, because there is nothing in a
// version string to predict from. It asks the toolchain whether the module
// still compiles, and when the answer changed from yes to no it puts back the
// indirect requirements this run raised -- the ones MVS would have resolved by
// itself -- and asks again.
//
// It defines three functions:
//
//   - autoupdate_go_indirect_versions, which prints "path version" for every
//     requirement marked `// indirect`;
//   - autoupdate_go_module_compiles, which answers whether the module in the
//     working directory type-checks;
//   - autoupdate_go_hold_breaking_bumps, which does the verify-hold-verify
//     pass, over autoupdate_go_compiled_before_upgrade and
//     autoupdate_go_restore_module.
//
// It never reverts an upgrade. What it holds back is only ever indirect, which
// nothing in the repository names and `go mod tidy` recomputes anyway. A direct
// dependency that broke the build is left exactly as it was written, so the
// pull request fails CI and a human is told.
//
// It depends on nothing else this package emits, deliberately: the major guard
// is emitted only when major upgrades are disallowed, and a guard that silently
// stopped working on the default path would be worse than no guard.
func GoCompileGuardScript() string {
	return goIndirectReaderScript() + goModuleCompilesScript() +
		goHoldBreakingBumpsScript() + goCompiledBeforeScript() + goRestoreModuleScript()
}

// goIndirectReaderScript emits the reader the guard decides from: the
// requirements `go get -u` moves without anyone naming them.
func goIndirectReaderScript() string {
	return `# autoupdate_go_indirect_versions <go.mod path>
# Prints "path version" for every requirement carrying a trailing
# "// indirect" marker, covering both the parenthesised require block and the
# single-line "require path version" form.
#
# Only the indirect ones, unlike autoupdate_go_module_versions: a direct
# requirement is what the pull request exists to move, and holding those back
# would leave a branch that updates nothing. Indirect requirements are the
# derived half of the graph -- "go mod tidy" recomputes them from what the
# direct ones ask for -- so putting one back costs the run nothing it meant to
# do.
autoupdate_go_indirect_versions() {
    awk '
        /^require[ \t]*\(/ { inblock = 1; next }
        inblock && /^\)/    { inblock = 0; next }
        {
            line = $0
            if (line !~ /\/\/[ \t]*indirect[ \t]*$/) next
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

// goModuleCompilesScript emits the question the whole guard is built on.
func goModuleCompilesScript() string {
	return `
# autoupdate_go_module_compiles <go binary>
# Answers whether the module in the working directory type-checks, test files
# included. Returns zero when it does.
#
# "go vet", not "go build". A module can hold no non-test Go files at all --
# an end-to-end harness is exactly that shape -- and for one of those
# "go build ./..." compiles nothing, finds nothing wrong and exits zero while
# "go test ./..." fails to build. That is not a corner case: it is what the
# Kubernetes harnesses in the doc comment above look like, and a build-only
# check is why the breakage reached them. "go vet" loads and type-checks every
# package including its test files, and runs nothing.
#
# Output is discarded because this is a predicate, asked up to three times per
# module; the caller prints what it decided. The failing compiler output is
# reproduced by the caller running "go mod tidy" and by CI on the branch.
autoupdate_go_module_compiles() {
    "$1" vet ./... > /dev/null 2>&1
}
`
}

// goHoldBreakingBumpsScript emits the verify-hold-verify pass itself.
func goHoldBreakingBumpsScript() string {
	return `
# autoupdate_go_hold_breaking_bumps <go binary> <go.mod snapshot> <go.sum snapshot>
# Verifies the upgraded module still compiles and, when it does not, puts back
# the indirect requirements this run raised.
#
# It never reverts the upgrade. Holding an indirect requirement back costs the
# run nothing, because nothing in the repository names one; a direct dependency
# that broke the build is news, and the pull request failing CI is how it gets
# delivered. Reverting that would leave a branch with nothing in it, and in a
# single-module repository the commit check would then find no changes and open
# no pull request at all -- turning a breaking upgrade into silence repeated on
# every scheduled run.
#
# The snapshots are the module as it stood after the go directive was settled
# and before "go get -u" ran, so this judges the dependency change and nothing
# else. They are put back only to ask whether the module compiled *before* the
# upgrade, and put back again straight after. A missing snapshot go.sum means
# the module had none, so restoring it removes the file rather than leaving the
# upgrade's behind.
#
# Returns non-zero when the module is still broken, so the caller can say so.
# Callers must branch on it rather than invoke it bare: the generated scripts
# run under "set -e", where a bare call would abort the run on the first module
# that failed and lose the modules queued behind it.
autoupdate_go_hold_breaking_bumps() {
    go_binary="$1"
    pre_mod="$2"
    pre_sum="$3"

    if autoupdate_go_module_compiles "$go_binary"; then
        echo "  the module compiles"
        return 0
    fi

    if ! autoupdate_go_compiled_before_upgrade "$go_binary" "$pre_mod" "$pre_sum"; then
        echo "  the module did not compile before this upgrade either; leaving the upgrade in place"
        return 0
    fi

    echo "  the upgrade stopped the module compiling; holding back the indirect requirements it raised"

    before_file="$(mktemp)"
    after_file="$(mktemp)"
    autoupdate_go_indirect_versions "$pre_mod" > "$before_file"
    autoupdate_go_indirect_versions go.mod > "$after_file"
    held=0

    while read -r module_path old_version; do
        [ -n "$module_path" ] || continue
        new_version="$(awk -v p="$module_path" '$1 == p { print $2; exit }' "$after_file")"
        [ -n "$new_version" ] || continue
        [ "$new_version" != "$old_version" ] || continue

        echo "    holding ${module_path} at ${old_version} (the upgrade raised it to ${new_version})"
        # "-require" rather than "-droprequire": it sets a floor, so a
        # requirement the repository had deliberately pinned above the graph --
        # a CVE fix nothing else demands yet -- stays where its owner put it.
        # Dropping it would hand the module back to MVS and undo that pin
        # silently, inside a pull request about something else.
        if ! "$go_binary" mod edit -require="${module_path}@${old_version}" 2>&1; then
            echo "      WARNING: could not hold ${module_path} at ${old_version}"
        fi
        held=1
    done < "$before_file"

    rm -f "$before_file" "$after_file"

    if [ "$held" = "1" ]; then
        echo "    re-running go mod tidy after holding the raised requirements..."
        "$go_binary" mod tidy 2>&1 || echo "      WARNING: go mod tidy had some errors"
    fi

    if autoupdate_go_module_compiles "$go_binary"; then
        echo "  the module compiles again with those held back"
        return 0
    fi

    # Something the hold does not reach broke it, and a direct dependency is
    # the overwhelmingly likely one. That is left exactly as "go get -u" wrote
    # it, deliberately: it is the only outcome here a human has to decide
    # about, and a pull request that fails CI is how they find out. Reverting
    # would produce a branch with nothing in it -- and when it is the only
    # module in the repository, the commit check finds no changes and no pull
    # request is opened at all, so a genuine breaking upgrade would be
    # swallowed on every run with a line in a log as its only trace.
    echo "  WARNING: the module still does not compile with only the indirect requirements held back"
    echo "           a direct dependency is the likely cause; leaving the upgrade in place so the pull request reports it"
    return 1
}
`
}

// goCompiledBeforeScript emits the probe the guard's whole judgement rests on.
//
// It is a separate function because it has an invariant of its own: it swaps
// the working copy to the pre-upgrade manifests and back, so it must leave the
// module exactly as it found it on every path out, including the failing one.
// Inlined into the decision it feeds, that obligation sat in the middle of an
// unrelated flow with two returns to get wrong.
func goCompiledBeforeScript() string {
	return `
# autoupdate_go_compiled_before_upgrade <go binary> <go.mod snapshot> <go.sum snapshot>
# Answers whether the module compiled BEFORE this run's dependency change,
# restoring the working copy to the upgraded manifests before it returns.
#
# The guard judges against the state the run started from rather than against
# "clean". A module can fail the check for reasons that have nothing to do with
# the upgrade -- a vet diagnostic someone has not got to yet, or a module that
# declares no packages at all -- and freezing its indirect requirements every
# run over one of those would be its own bug. Only a yes that turned into a no
# is this run's doing.
autoupdate_go_compiled_before_upgrade() {
    go_binary="$1"
    pre_mod="$2"
    pre_sum="$3"

    post_mod="$(mktemp)"
    post_sum="$(mktemp)"
    cp go.mod "$post_mod"
    if [ -f go.sum ]; then cp go.sum "$post_sum"; else rm -f "$post_sum"; fi

    autoupdate_go_restore_module "$pre_mod" "$pre_sum"
    if autoupdate_go_module_compiles "$go_binary"; then
        compiled_before=0
    else
        compiled_before=1
    fi
    autoupdate_go_restore_module "$post_mod" "$post_sum"

    rm -f "$post_mod" "$post_sum"
    return "$compiled_before"
}
`
}

// goRestoreModuleScript emits the one place a snapshot is put back. The guard
// swaps manifests four times over three states, and a restore that handled the
// two files separately at each call site is a way to commit a go.mod against
// somebody else's go.sum.
func goRestoreModuleScript() string {
	return `
# autoupdate_go_restore_module <go.mod snapshot> <go.sum snapshot>
# Puts a snapshot back over the working copy. A snapshot go.sum that is not
# there records a module that had none, so the restore removes the file rather
# than leaving the upgrade's behind.
autoupdate_go_restore_module() {
    cp "$1" go.mod
    if [ -f "$2" ]; then cp "$2" go.sum; else rm -f go.sum; fi
}
`
}
