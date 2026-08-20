//go:build unit

package java_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	javaUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/java"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
	"github.com/rios0rios0/autoupdate/test/infrastructure/scriptrunner"
)

// javaDowngradeRepo is the repository every case in this file resolves against.
var javaDowngradeRepo = entities.Repository{ //nolint:gochecknoglobals // shared test fixture
	Organization: "org",
	Name:         "repo",
}

func TestJavaVersionPinIsNeverDowngraded(t *testing.T) {
	t.Parallel()

	cases := []scriptrunner.PinCase{
		{
			// The feed reports the newest LTS; this repository is past it.
			Name:   "keep the pin when the repository runs a JDK newer than the LTS",
			File:   ".java-version",
			Pinned: "25", Want: "25",
			Marker: "JAVA_VERSION_UPDATED=false",
		},
		{
			Name:   "raise the pin when the repository is behind the LTS",
			File:   ".java-version",
			Pinned: "17", Want: "21.0.5",
			Marker: "JAVA_VERSION_UPDATED=true",
		},
	}

	var sb strings.Builder
	javaUpdater.WriteJavaUpgradeCommands(&sb, javaUpdater.UpgradeParamsExported{})
	opts := scriptrunner.Options{
		Env:   map[string]string{"JAVA_VERSION": "21.0.5", "BUILD_SYSTEM": "gradle"},
		Stubs: []string{"gradle", "mvn", "java"},
	}
	for _, testCase := range cases {
		t.Run("should "+testCase.Name, func(t *testing.T) {
			t.Parallel()

			scriptrunner.AssertVersionPin(t, sb.String(), opts, testCase)
		})
	}
}

func TestJavaDockerfileTagIsNeverDowngraded(t *testing.T) {
	t.Parallel()

	cases := []scriptrunner.ImageCase{
		{
			Name:       "keep a base image newer than the version being rolled out",
			Dockerfile: "FROM eclipse-temurin:25-jdk\n",
			Want:       []string{"FROM eclipse-temurin:25-jdk"},
		},
		{
			Name:       "upgrade a base image older than the version being rolled out",
			Dockerfile: "FROM eclipse-temurin:17-jdk\n",
			Want:       []string{"FROM eclipse-temurin:21-jdk"},
		},
		{
			Name:       "upgrade every JDK vendor it knows",
			Dockerfile: "FROM openjdk:17-slim AS a\nFROM amazoncorretto:17 AS b\n",
			Want:       []string{"FROM openjdk:21-slim", "FROM amazoncorretto:21"},
		},
	}

	var sb strings.Builder
	javaUpdater.WriteDockerfileUpdate(&sb)
	opts := scriptrunner.Options{
		Env: map[string]string{"JAVA_VERSION": "21.0.5", "JAVA_VERSION_CHANGED": "true"},
	}
	for _, testCase := range cases {
		t.Run("should "+testCase.Name, func(t *testing.T) {
			t.Parallel()

			scriptrunner.AssertDockerfileTags(t, sb.String(), opts, testCase)
		})
	}
}

func TestJavaVersionContextIsNeverADowngrade(t *testing.T) {
	t.Parallel()

	t.Run("should report no version upgrade when the pin is ahead of the LTS", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.SpyProviderWithFile(".java-version", "25\n")

		// when
		vCtx := javaUpdater.ResolveVersionContext(t.Context(), provider, javaDowngradeRepo, "21.0.5")

		// then
		assert.False(t, vCtx.NeedsVersionUpgrade)
	})

	t.Run("should report a version upgrade when the pin is behind the LTS", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.SpyProviderWithFile(".java-version", "17\n")

		// when
		vCtx := javaUpdater.ResolveVersionContext(t.Context(), provider, javaDowngradeRepo, "21.0.5")

		// then
		assert.True(t, vCtx.NeedsVersionUpgrade)
	})
}
