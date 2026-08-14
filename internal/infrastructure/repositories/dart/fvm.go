package dart

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FvmConfigFile is FVM's per-project SDK pin. It is the Flutter analogue of
// `.ruby-version` or `.nvmrc`: the one file in a repository that names the SDK
// the project is built with.
//
// A pure Dart package has no equivalent, and `environment: sdk:` in pubspec.yaml
// is deliberately left alone — raising a package's SDK floor is a compatibility
// decision for its consumers, not a maintenance chore, and `pub upgrade
// --major-versions` already raises it when a dependency actually requires it.
const FvmConfigFile = ".fvmrc"

// fvmConfigFileMode keeps the rewritten pin readable only by its owner.
const fvmConfigFileMode = 0o600

// ParseFvmVersion returns the Flutter SDK version pinned by a .fvmrc document.
func ParseFvmVersion(content string) string {
	var config struct {
		Flutter string `json:"flutter"`
	}
	if err := json.Unmarshal([]byte(content), &config); err != nil {
		return ""
	}
	return config.Flutter
}

// WriteFvmVersion rewrites the "flutter" key of a .fvmrc document, leaving the
// rest of it — flavors, and anything a newer FVM adds — untouched. It reports
// whether the file changed.
func WriteFvmVersion(repoDir, version string) (bool, error) {
	path := filepath.Join(repoDir, FvmConfigFile)
	raw, err := os.ReadFile(path) // #nosec G304 -- path is rooted at the cloned repository
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", FvmConfigFile, err)
	}

	// A map preserves the keys this code does not know about; the round-trip
	// reformats the document, which is acceptable for generated FVM config in a
	// way it would not be for a hand-written manifest.
	var config map[string]json.RawMessage
	if unmarshalErr := json.Unmarshal(raw, &config); unmarshalErr != nil {
		return false, fmt.Errorf("parsing %s: %w", FvmConfigFile, unmarshalErr)
	}

	if current := ParseFvmVersion(string(raw)); current == version {
		return false, nil
	}

	encoded, err := json.Marshal(version)
	if err != nil {
		return false, fmt.Errorf("encoding version: %w", err)
	}
	config["flutter"] = encoded

	out, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return false, fmt.Errorf("encoding %s: %w", FvmConfigFile, err)
	}
	if writeErr := os.WriteFile(path, append(out, '\n'), fvmConfigFileMode); writeErr != nil {
		return false, fmt.Errorf("writing %s: %w", FvmConfigFile, writeErr)
	}
	return true, nil
}
