package support

import (
	"fmt"
	"strings"
)

// VersionPinUpdate describes one ecosystem's version pin file rewrite.
type VersionPinUpdate struct {
	// File is the pin file, relative to the repository root.
	File string

	// Subject names the language in the comment and the log lines.
	Subject string

	// VersionVar is the shell variable holding the fetched version.
	VersionVar string

	// CurrentVar is the shell variable the current pin is read into. It differs
	// per ecosystem only because the generated scripts already named it, and the
	// name reaches the log lines.
	CurrentVar string

	// ChangedVar is the shell flag set when the pin was rewritten; the
	// Dockerfile rewrite is gated on it.
	ChangedVar string

	// MarkerVar prefixes the `<MARKER>_UPDATED=` line the Go side greps for to
	// learn whether the pin actually moved.
	MarkerVar string
}

// VersionPinUpdateScript emits the guarded rewrite of a single-line version pin
// file — `.java-version`, `.python-version`, `.ruby-version`.
//
// These three differ only in their file name and variable names, so they share
// one emitter for the same reason the Dockerfile rewrites do: the `echo` that
// overwrites the file compares nothing, and only the surrounding
// `autoupdate_version_is_newer` keeps a repository pinned ahead of the release
// feed from being rolled back. One spelling of that guard is one place to get
// it right.
//
// JavaScript is not a caller: it maintains two pin files at once and loops. C#
// is not either: its pin lives inside `global.json` and is read with `jq` or
// `python3` rather than `head`.
func VersionPinUpdateScript(update VersionPinUpdate) string {
	var sb strings.Builder

	sb.WriteString(VersionGuardScript())

	fmt.Fprintf(&sb, "# Check and update the %s version\n", update.Subject)
	fmt.Fprintf(&sb, "%s=false\n", update.ChangedVar)
	fmt.Fprintf(&sb,
		"if [ -n \"${%s:-}\" ] && [ -f \"%s\" ]; then\n",
		update.VersionVar, update.File,
	)
	fmt.Fprintf(&sb,
		"    %s=$(head -1 %s | tr -d '[:space:]')\n",
		update.CurrentVar, update.File,
	)
	fmt.Fprintf(&sb,
		"    if autoupdate_version_is_newer \"$%s\" \"$%s\"; then\n",
		update.VersionVar, update.CurrentVar,
	)
	fmt.Fprintf(&sb,
		"        echo \"Updating %s from $%s to $%s...\"\n",
		update.File, update.CurrentVar, update.VersionVar,
	)
	fmt.Fprintf(&sb, "        echo \"$%s\" > %s\n", update.VersionVar, update.File)
	fmt.Fprintf(&sb, "        %s=true\n", update.ChangedVar)
	fmt.Fprintf(&sb, "        echo \"%s_UPDATED=true\"\n", update.MarkerVar)
	sb.WriteString("    else\n")
	fmt.Fprintf(&sb,
		"        echo \"Keeping %s at $%s (not older than $%s)\"\n",
		update.File, update.CurrentVar, update.VersionVar,
	)
	fmt.Fprintf(&sb, "        echo \"%s_UPDATED=false\"\n", update.MarkerVar)
	sb.WriteString("    fi\n")
	sb.WriteString("else\n")
	fmt.Fprintf(&sb, "    echo \"%s_UPDATED=false\"\n", update.MarkerVar)
	sb.WriteString("fi\n\n")

	return sb.String()
}
