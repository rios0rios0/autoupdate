//go:build unit

package javascript_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	jsUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/javascript"
	"github.com/rios0rios0/autoupdate/test/infrastructure/scriptrunner"
)

// nodePinOptions runs the pin block with the package managers stubbed out, so
// the case exercises the rewrite and nothing else.
func nodePinOptions(nodeVersion string) scriptrunner.Options {
	return scriptrunner.Options{
		Env:   map[string]string{"NODE_VERSION": nodeVersion, "PACKAGE_MANAGER": "npm"},
		Stubs: []string{"npm", "pnpm", "yarn"},
	}
}

func TestNodeVersionPinIsNeverDowngraded(t *testing.T) {
	t.Parallel()

	// The reported defect: a repository on a current release had both pins
	// rewritten to the LTS the feed reports. See medhub-tech/frontend#43.
	cases := []scriptrunner.PinCase{
		{
			Name:   "keep the pin when the repository tracks a release newer than the LTS line",
			File:   ".nvmrc",
			Pinned: "26.7.0", Want: "26.7.0",
			Marker: "NODE_VERSION_UPDATED=false",
		},
		{
			Name:   "keep .node-version alongside .nvmrc",
			File:   ".node-version",
			Pinned: "26.7.0", Want: "26.7.0",
			Marker: "NODE_VERSION_UPDATED=false",
		},
		{
			Name:   "raise the pin when the repository is behind the latest release",
			File:   ".nvmrc",
			Pinned: "20.11.1", Want: "24.19.0",
			Marker: "NODE_VERSION_UPDATED=true",
		},
		{
			Name:   "keep the pin when it already names the latest release",
			File:   ".nvmrc",
			Pinned: "24.19.0", Want: "24.19.0",
			Marker: "NODE_VERSION_UPDATED=false",
		},
		{
			// "lts/*" is a deliberate choice, not a stale version number.
			Name:   "keep an alias pin that names no version at all",
			File:   ".nvmrc",
			Pinned: "lts/*", Want: "lts/*",
			Marker: "NODE_VERSION_UPDATED=false",
		},
	}

	fragment := jsUpdater.WriteJSUpgradeCommands(jsUpdater.UpgradeParams{})
	for _, testCase := range cases {
		t.Run("should "+testCase.Name, func(t *testing.T) {
			t.Parallel()

			scriptrunner.AssertVersionPin(t, fragment, nodePinOptions("24.19.0"), testCase)
		})
	}
}

func TestNodeDockerfileTagIsNeverDowngraded(t *testing.T) {
	t.Parallel()

	cases := []scriptrunner.ImageCase{
		{
			Name:       "keep a base image newer than the version being rolled out",
			Dockerfile: "FROM node:26-alpine\nRUN npm ci\n",
			Want:       []string{"FROM node:26-alpine"},
		},
		{
			Name:       "upgrade a base image older than the version being rolled out",
			Dockerfile: "FROM node:20-alpine\nRUN npm ci\n",
			Want:       []string{"FROM node:24.19.0-alpine"},
		},
		{
			// Rewriting the older stage alone would still move the runtime
			// stage backwards while claiming an upgrade.
			Name:       "keep every stage when any one of them is already ahead",
			Dockerfile: "FROM node:20-alpine AS build\nFROM node:26-alpine AS runtime\n",
			Want:       []string{"FROM node:20-alpine AS build", "FROM node:26-alpine AS runtime"},
		},
		{
			Name:       "leave a Dockerfile pinning no numeric node tag alone",
			Dockerfile: "FROM node:alpine\n",
			Want:       []string{"FROM node:alpine"},
		},
	}

	fragment := jsUpdater.WriteDockerfileUpdate()
	opts := scriptrunner.Options{
		Env: map[string]string{"NODE_VERSION": "24.19.0", "NODE_VERSION_CHANGED": "true"},
	}
	for _, testCase := range cases {
		t.Run("should "+testCase.Name, func(t *testing.T) {
			t.Parallel()

			scriptrunner.AssertDockerfileTags(t, fragment, opts, testCase)
		})
	}
}

func TestNodeVersionBlockUsesTheSharedGuard(t *testing.T) {
	t.Parallel()

	t.Run("should compare the target against the pin rather than test inequality", func(t *testing.T) {
		t.Parallel()

		// given
		script := jsUpdater.WriteJSUpgradeCommands(jsUpdater.UpgradeParams{})

		// when
		usesGuard := strings.Contains(script,
			"autoupdate_version_is_newer \"$NODE_VERSION\" \"$CURRENT_NODE_VERSION\"")

		// then
		assert.True(t, usesGuard)
		assert.NotContains(t, script, "\"$CURRENT_NODE_VERSION\" != \"$NODE_VERSION\"")
	})
}
