// Package configs carries the configuration AutoUpdate ships with itself.
//
// It is data and nothing else. The declaration has to live beside the file because the
// embed directive cannot reach a parent directory, and the file has to keep its path
// because entities.DefaultConfigURL serves this same document from `main` -- so the
// built-in and the published defaults can never describe different things.
package configs

import _ "embed"

// Default is configs/autoupdate.yaml as of the build. It is the first configuration
// layer, so a binary with no configuration of its own and no network still knows every
// updater AutoUpdate supports.
//
//go:embed autoupdate.yaml
var Default []byte
