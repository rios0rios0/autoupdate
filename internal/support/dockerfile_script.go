package support

import (
	"fmt"
	"strings"
)

// dottedTagPattern matches the numeric part of a tag written with dots, which
// is how almost every base image spells its version ("3.13", "24.19.0"). Java's
// images pin a bare major instead, so those callers override it.
const dottedTagPattern = "[0-9][0-9.]*"

// MajorOnlyTagPattern matches a tag that carries a bare major version, which is
// how the JDK images are published ("eclipse-temurin:21-jdk"). Callers pass it
// as DockerfileImage.TagPattern to override the dotted default.
const MajorOnlyTagPattern = "[0-9][0-9]*"

// DockerfileImage names one base image whose tag the rewrite loop maintains.
type DockerfileImage struct {
	// Name is the image exactly as it appears before the colon, for example
	// "node" or "mcr.microsoft.com/dotnet/sdk".
	Name string

	// Label distinguishes this image in the log when one Dockerfile pins
	// several of them (".NET SDK" against "ASP.NET"). An empty Label logs the
	// image name.
	Label string

	// TagPattern is the basic regular expression matching the numeric part of
	// the tag. An empty TagPattern means dottedTagPattern.
	TagPattern string
}

// DockerfileTagUpdate describes one ecosystem's base-image rewrite.
type DockerfileTagUpdate struct {
	// ChangedVar is the shell flag the pin rewrite sets; the whole block is
	// gated on it, so images move only when the version pin itself moved.
	ChangedVar string

	// VersionVar is the shell variable holding the version to write into the
	// tag. It is not always the fetched version: Java pins a bare major and
	// .NET a major.minor, both derived by Prelude.
	VersionVar string

	// Subject names the ecosystem in the comment and the log line.
	Subject string

	// Prelude is optional bash emitted inside the guard, before the walk —
	// where a caller derives VersionVar from the fetched version.
	Prelude string

	// Images are rewritten in order, each guarded independently.
	Images []DockerfileImage
}

// DockerfileTagUpdateScript emits the guarded rewrite of every base image tag
// in every Dockerfile of the clone.
//
// The five ecosystems that rewrite base images differ only in this data — the
// image names, the variable holding the version, and how that version is
// derived — so they share one emitter. Keeping five hand-copied versions of the
// walk is how the guard came to be missing from some of them in the first
// place: each `sed` rewrites unconditionally, and only the surrounding
// `autoupdate_image_tag_is_older` stops a Dockerfile already on a newer base
// image from being moved backwards.
func DockerfileTagUpdateScript(update DockerfileTagUpdate) string {
	var sb strings.Builder

	// Emitted here as well as by the pin rewrite, so this fragment carries the
	// comparison it depends on instead of inheriting it from whatever ran first.
	sb.WriteString(VersionGuardScript())

	fmt.Fprintf(&sb,
		"# Update the Dockerfile %s base image tags when the version was bumped.\n",
		update.Subject,
	)
	fmt.Fprintf(&sb, "if [ \"$%s\" = \"true\" ]; then\n", update.ChangedVar)
	sb.WriteString(update.Prelude)
	fmt.Fprintf(&sb,
		"    echo \"Updating Dockerfile %s image tags to $%s...\"\n",
		update.Subject, update.VersionVar,
	)
	sb.WriteString(
		"    find . -type f -not -path './.git/*' " +
			"\\( -name 'Dockerfile' -o -name 'Dockerfile.*' -o -name '*.Dockerfile' \\) " +
			"-print0 | while IFS= read -r -d '' df; do\n",
	)

	for _, image := range update.Images {
		writeImageRewrite(&sb, update.VersionVar, image)
	}

	sb.WriteString("    done\n")
	sb.WriteString("fi\n\n")

	return sb.String()
}

// writeImageRewrite emits the guarded `sed` for a single image.
func writeImageRewrite(sb *strings.Builder, versionVar string, image DockerfileImage) {
	pattern := image.TagPattern
	if pattern == "" {
		pattern = dottedTagPattern
	}

	label := image.Label
	if label == "" {
		label = image.Name
	}

	fmt.Fprintf(sb,
		"        if autoupdate_image_tag_is_older \"$df\" \"%s\" \"$%s\" '%s'; then\n",
		image.Name, versionVar, pattern,
	)
	fmt.Fprintf(sb,
		"            sed \"s|%s:%s|%s:${%s}|g\" \"$df\" > \"$df.tmp\" && mv \"$df.tmp\" \"$df\"\n",
		image.Name, pattern, image.Name, versionVar,
	)
	fmt.Fprintf(sb, "            echo \"  Updated %s in $df\"\n", label)
	sb.WriteString("        fi\n")
}
