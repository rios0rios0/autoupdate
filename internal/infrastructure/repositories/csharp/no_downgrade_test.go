//go:build unit

package csharp_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	csUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/csharp"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
	"github.com/rios0rios0/autoupdate/test/infrastructure/scriptrunner"
)

// dotnetDowngradeRepo is the repository every case in this file resolves against.
var dotnetDowngradeRepo = entities.Repository{ //nolint:gochecknoglobals // shared test fixture
	Organization: "org",
	Name:         "repo",
}

func globalJSONWith(version string) string {
	return "{\n  \"sdk\": {\n    \"version\": \"" + version + "\"\n  }\n}\n"
}

func TestDotnetDockerfileTagIsNeverDowngraded(t *testing.T) {
	t.Parallel()

	cases := []scriptrunner.ImageCase{
		{
			Name: "keep base images newer than the SDK being rolled out",
			Dockerfile: "FROM mcr.microsoft.com/dotnet/sdk:10.0 AS build\n" +
				"FROM mcr.microsoft.com/dotnet/aspnet:10.0 AS runtime\n",
			Want: []string{
				"mcr.microsoft.com/dotnet/sdk:10.0",
				"mcr.microsoft.com/dotnet/aspnet:10.0",
			},
		},
		{
			Name: "upgrade base images older than the SDK being rolled out",
			Dockerfile: "FROM mcr.microsoft.com/dotnet/sdk:6.0 AS build\n" +
				"FROM mcr.microsoft.com/dotnet/runtime:6.0 AS runtime\n",
			Want: []string{
				"mcr.microsoft.com/dotnet/sdk:8.0",
				"mcr.microsoft.com/dotnet/runtime:8.0",
			},
		},
	}

	var sb strings.Builder
	csUpdater.WriteDockerfileUpdate(&sb)
	opts := scriptrunner.Options{
		Env: map[string]string{"DOTNET_VERSION": "8.0.404", "DOTNET_VERSION_CHANGED": "true"},
	}
	for _, testCase := range cases {
		t.Run("should "+testCase.Name, func(t *testing.T) {
			t.Parallel()

			scriptrunner.AssertDockerfileTags(t, sb.String(), opts, testCase)
		})
	}
}

func TestDotnetVersionContextIsNeverADowngrade(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		pinned string
		want   bool
	}{
		{"report no version upgrade when global.json is ahead of the feed", "10.0.100", false},
		// An SDK preview is deliberately ahead of the stable channel.
		{"report no version upgrade when a release replaces a newer pre-release", "9.0.100-rc.2.24474.11", false},
		{"report a version upgrade when global.json is behind the feed", "6.0.400", true},
	}

	for _, testCase := range cases {
		t.Run("should "+testCase.name, func(t *testing.T) {
			t.Parallel()

			// given
			provider := repositorydoubles.SpyProviderWithFile("global.json", globalJSONWith(testCase.pinned))

			// when
			vCtx := csUpdater.ResolveVersionContext(t.Context(), provider, dotnetDowngradeRepo, "8.0.404")

			// then
			assert.Equal(t, testCase.want, vCtx.NeedsVersionUpgrade)
		})
	}
}
