package entities

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// chlog (https://github.com/luizjhonata/chlog) is a fragment-based changelog
// tool: instead of editing a shared CHANGELOG.md, every change is written to
// its own YAML file under .changes/unreleased/, which removes changelog merge
// conflicts entirely. The tool later compiles those fragments into ordinary
// Keep a Changelog sections.
//
// A chlog repository therefore keeps [Unreleased] permanently empty, and the
// entry autoupdate would append there is exactly the kind of edit chlog exists
// to avoid: two updaters running against the same repository would conflict on
// the same lines. When chlog is detected, autoupdate emits a fragment instead.
//
// chlog is a command-only Go module (everything lives under internal/), so its
// format is re-implemented here rather than imported.

// chlog configuration file names, in the order chlog itself probes them.
const (
	ChlogConfigFile    = ".chlog.yaml"
	ChlogAltConfigFile = ".chlog.yml"
)

// Defaults mirroring chlog's DefaultConfig(). They apply whenever a repository
// uses chlog without committing a configuration file, which chlog supports.
const (
	DefaultChlogChangesDir    = ".changes"
	DefaultChlogUnreleasedDir = "unreleased"
	DefaultChlogChangelogPath = "CHANGELOG.md"
)

// ChlogUpdateKind is the fragment kind autoupdate files its entries under.
// gitforge's InsertChangelogEntry writes into "### Changed", so using the same
// bucket keeps the released notes identical whichever format a repository uses.
const ChlogUpdateKind = "Changed"

// chlogRandomSuffixBytes matches chlog's own fragment naming: a nanosecond
// timestamp plus four hex characters, which keeps names unique even when
// several fragments are written inside the same nanosecond.
const chlogRandomSuffixBytes = 2

var (
	// ErrChlogPathEscapesRepo is returned when .chlog.yaml configures a path
	// that would take autoupdate outside the repository it was pointed at.
	ErrChlogPathEscapesRepo = errors.New("chlog path escapes the repository root")

	// ErrChlogNoKinds is returned when .chlog.yaml declares an empty kind list,
	// which chlog itself rejects as invalid.
	ErrChlogNoKinds = errors.New("chlog kinds list must not be empty")
)

// ChlogKind is a single entry of the "kinds" list in .chlog.yaml. Only Label
// matters here: autoupdate always files under the "Changed" kind, so chlog's
// Auto mapping (which drives version bumps) is deliberately not applied.
type ChlogKind struct {
	Label string `yaml:"label"`
	Auto  string `yaml:"auto,omitempty"`
}

// ChlogConfig is the subset of .chlog.yaml that autoupdate needs. The rendering
// templates (versionFormat, kindFormat, changeFormat) are ignored, since
// autoupdate never compiles fragments -- it only writes them.
type ChlogConfig struct {
	ChangesDir    string      `yaml:"changesDir"`
	UnreleasedDir string      `yaml:"unreleasedDir"`
	ChangelogPath string      `yaml:"changelogPath"`
	Kinds         []ChlogKind `yaml:"kinds"`
}

// ChlogFragment is one pending change, as stored in
// .changes/unreleased/<timestamp>-<random>.yaml.
type ChlogFragment struct {
	Kind string    `yaml:"kind"`
	Body string    `yaml:"body"`
	Time time.Time `yaml:"time"`
}

// DefaultChlogConfig returns chlog's own defaults.
func DefaultChlogConfig() ChlogConfig {
	return ChlogConfig{
		ChangesDir:    DefaultChlogChangesDir,
		UnreleasedDir: DefaultChlogUnreleasedDir,
		ChangelogPath: DefaultChlogChangelogPath,
		Kinds: []ChlogKind{
			{Label: "Added", Auto: "minor"},
			{Label: "Changed", Auto: "major"},
			{Label: "Deprecated", Auto: "minor"},
			{Label: "Removed", Auto: "major"},
			{Label: "Fixed", Auto: "patch"},
			{Label: "Security", Auto: "patch"},
		},
	}
}

// ParseChlogConfig decodes raw .chlog.yaml bytes, filling any unset field from
// chlog's defaults. Empty input yields the defaults, matching chlog's behaviour
// for a repository that uses the tool without committing a configuration file.
//
// Unknown keys are tolerated: autoupdate only reads a subset of the schema.
//
// The error deliberately does not name a file: the caller knows whether these
// bytes came from .chlog.yaml, .chlog.yml, or a provider API, and wraps the
// error with the path it actually read.
func ParseChlogConfig(data []byte) (*ChlogConfig, error) {
	config := DefaultChlogConfig()
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("failed to parse the chlog configuration: %w", err)
		}
	}

	applyChlogDefaults(&config)
	if err := validateChlogConfig(&config); err != nil {
		return nil, err
	}
	return &config, nil
}

// applyChlogDefaults restores chlog's defaults for keys the file left out and
// normalizes the configured paths to forward slashes. An explicitly empty value
// is treated as absent rather than as "the repository root", which is what chlog
// does when it merges a file over its defaults.
func applyChlogDefaults(config *ChlogConfig) {
	config.ChangesDir = normalizeChlogPath(config.ChangesDir, DefaultChlogChangesDir)
	config.UnreleasedDir = normalizeChlogPath(config.UnreleasedDir, DefaultChlogUnreleasedDir)
	config.ChangelogPath = normalizeChlogPath(config.ChangelogPath, DefaultChlogChangelogPath)
	if len(config.Kinds) == 0 {
		config.Kinds = DefaultChlogConfig().Kinds
	}
}

// normalizeChlogPath returns the configured value in forward-slash form, or the
// fallback when the file left the key out.
//
// The conversion is unconditional rather than [filepath.ToSlash], which only
// rewrites the separator of the *running* platform and is therefore a no-op on
// Linux. A configuration is committed once and read wherever autoupdate happens
// to run, so a Windows-authored `docs\changes` has to mean the same directory on
// both — and, more importantly, `..\..\etc` has to be recognised as an escape by
// the validation below no matter which platform validates it.
func normalizeChlogPath(value, fallback string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
	if normalized == "" {
		return fallback
	}
	return normalized
}

// validateChlogConfig rejects configured paths that leave the repository root.
//
// .chlog.yaml is committed by the repository being updated, and in batch mode
// autoupdate processes repositories it does not own, so these values are
// untrusted input. They drive the path autoupdate writes fragments to -- both
// on disk in local mode and through the provider API in batch mode -- so an
// absolute or parent-escaping value would let a hostile configuration write
// outside the repository.
func validateChlogConfig(config *ChlogConfig) error {
	if len(config.Kinds) == 0 {
		return ErrChlogNoKinds
	}

	fields := []struct{ name, value string }{
		{"changesDir", config.ChangesDir},
		{"unreleasedDir", config.UnreleasedDir},
		{"changelogPath", config.ChangelogPath},
		// The directories are joined, so a pair that is individually harmless
		// but escapes once combined has to be rejected too.
		{"changesDir/unreleasedDir", path.Join(config.ChangesDir, config.UnreleasedDir)},
	}

	for _, field := range fields {
		if !isPathInsideRepo(field.value) {
			return fmt.Errorf("%w: %s %q must be relative to the repository root",
				ErrChlogPathEscapesRepo, field.name, field.value)
		}
	}
	return nil
}

// isPathInsideRepo reports whether a configured path stays within the repository
// root. An absolute path, "..", or anything reaching through ".." would address
// a file autoupdate was never pointed at.
//
// The value has already been normalized to forward slashes by
// [normalizeChlogPath], which is what makes this decision independent of the
// platform doing the validating: `..\..\etc` reaches the ".." check as
// `../../etc` on Linux too, and `\tmp\evil` arrives as the absolute `/tmp/evil`
// rather than as a single oddly-named element. Both are rejected either way.
func isPathInsideRepo(value string) bool {
	if value == "" {
		return false
	}
	// A Windows drive-relative path such as "C:changes", and the "C:/changes"
	// form, are absolute on Windows but not recognised as such by [path].
	if strings.Contains(value, ":") {
		return false
	}

	clean := path.Clean(value)
	if path.IsAbs(clean) || filepath.IsAbs(clean) {
		return false
	}
	return clean != ".." && !strings.HasPrefix(clean, "../")
}

// UnreleasedPath returns the repository-relative directory holding the pending
// fragments. Parsing normalized both components to forward slashes, so the
// result can be used as a provider API path directly and, after conversion by
// [ChlogFragmentDiskPath], as an on-disk path.
func (c *ChlogConfig) UnreleasedPath() string {
	return path.Join(c.ChangesDir, c.UnreleasedDir)
}

// KindLabel resolves the label a repository uses for the given kind. chlog
// matches kinds case-insensitively and a repository may rename the default
// labels, so the configured spelling is returned when one matches. A repository
// that dropped the kind entirely still gets the canonical label: chlog renders
// unknown kinds as their own trailing section rather than dropping them, which
// keeps the entry visible instead of silently losing it.
func (c *ChlogConfig) KindLabel(kind string) string {
	for _, configured := range c.Kinds {
		if strings.EqualFold(strings.TrimSpace(configured.Label), strings.TrimSpace(kind)) {
			return configured.Label
		}
	}
	return kind
}

// NewChlogFragments turns Keep a Changelog bullet lines into chlog fragments,
// one file per entry, and returns them as file changes ready to be written to
// disk or committed through a provider. The entries are the same strings that
// would otherwise be inserted under [Unreleased] / ### Changed, so callers do
// not need to know which format the repository uses.
//
// An entry that carries no text once its bullet marker is stripped is skipped:
// an empty fragment would render as an empty bullet at release time.
func (c *ChlogConfig) NewChlogFragments(entries []string, at time.Time) ([]FileChange, error) {
	changes := make([]FileChange, 0, len(entries))

	for _, entry := range entries {
		body := StripBulletPrefix(entry)
		if body == "" {
			continue
		}

		content, err := renderChlogFragment(c.KindLabel(ChlogUpdateKind), body, at)
		if err != nil {
			return nil, err
		}

		name, err := newChlogFragmentName(at)
		if err != nil {
			return nil, err
		}

		changes = append(changes, FileChange{
			Path:       path.Join(c.UnreleasedPath(), name),
			Content:    content,
			ChangeType: "add",
		})
	}

	return changes, nil
}

// StripBulletPrefix removes the leading Markdown bullet marker from a changelog
// entry. Fragment bodies are stored without one -- chlog adds it back through
// changeFormat -- so keeping it would render as "- - text" after a release.
func StripBulletPrefix(entry string) string {
	trimmed := strings.TrimSpace(entry)
	if after, found := strings.CutPrefix(trimmed, "-"); found {
		return strings.TrimSpace(after)
	}
	return trimmed
}

// renderChlogFragment marshals one fragment into chlog's on-disk YAML shape.
func renderChlogFragment(kind, body string, at time.Time) (string, error) {
	data, err := yaml.Marshal(&ChlogFragment{
		Kind: kind,
		Body: body,
		Time: at.UTC(),
	})
	if err != nil {
		return "", fmt.Errorf("failed to render the chlog fragment: %w", err)
	}
	return string(data), nil
}

// newChlogFragmentName builds a fragment file name in chlog's own format:
// "<unix-nanoseconds>-<four hex characters>.yaml".
func newChlogFragmentName(at time.Time) (string, error) {
	suffix := make([]byte, chlogRandomSuffixBytes)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("failed to generate the chlog fragment name: %w", err)
	}
	return fmt.Sprintf("%d-%s.yaml", at.UnixNano(), hex.EncodeToString(suffix)), nil
}

// ChlogFragmentDiskPath converts a repository-relative fragment path into an
// absolute path under repoDir, using the host separator.
func ChlogFragmentDiskPath(repoDir, fragmentPath string) string {
	return filepath.Join(repoDir, filepath.FromSlash(fragmentPath))
}

// ChlogFragmentDirMode is the permission applied to the fragment directory when
// autoupdate has to create it. A directory needs the owner execute (search) bit,
// so 0o700 -- not the 0o600 used for files -- is the least-privilege mode.
const ChlogFragmentDirMode os.FileMode = 0o700
