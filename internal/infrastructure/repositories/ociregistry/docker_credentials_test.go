package ociregistry_test

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/ociregistry"
)

// helperCall is one credential helper invocation a test observed.
type helperCall struct {
	helper    string
	serverURL string
}

// credentialSources builds the store's sources over an environment map, a home directory and
// a helper runner that answers output and err and records what it was asked.
func credentialSources(
	env map[string]string, home string, output string, err error, calls *[]helperCall,
) ociregistry.DockerCredentialSources {
	return ociregistry.DockerCredentialSources{
		LookupEnv: func(key string) (string, bool) {
			value, ok := env[key]
			return value, ok
		},
		HomeDir: func() (string, error) { return home, nil },
		RunHelper: func(_ context.Context, helper, serverURL string) ([]byte, error) {
			*calls = append(*calls, helperCall{helper: helper, serverURL: serverURL})
			return []byte(output), err
		},
	}
}

// writeDockerConfig writes config.json into a new directory and returns the directory.
func writeDockerConfig(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte(content), 0o600))
	return dir
}

// fixtureBasicAuth is the `auth` value of an `auths` entry holding the fixture credential.
func fixtureBasicAuth() string {
	return base64.StdEncoding.EncodeToString([]byte(fixtureUser + ":" + fixturePassword))
}

func TestDockerCredentialStoreLookup(t *testing.T) {
	t.Parallel()

	t.Run("should read a base64 auth entry from the configuration DOCKER_CONFIG names", func(t *testing.T) {
		t.Parallel()

		// given
		dir := writeDockerConfig(t, `{"auths":{"registry.example.com":{"auth":"`+
			fixtureBasicAuth()+`"}}}`)
		var calls []helperCall
		store := ociregistry.NewDockerCredentialStoreWithDeps(
			credentialSources(map[string]string{"DOCKER_CONFIG": dir}, "", "", nil, &calls))

		// when
		credential, err := store.Lookup(t.Context(), "registry.example.com")

		// then
		require.NoError(t, err)
		assert.Equal(t, ociregistry.Credential{Username: fixtureUser, Password: fixturePassword}, credential)
		assert.Empty(t, calls)
	})

	t.Run("should read an identity token from an entry keyed by a URL", func(t *testing.T) {
		t.Parallel()

		// given
		dir := writeDockerConfig(t, `{"auths":{"https://Registry.Example.com/v2/":{"identitytoken":"`+
			fixtureIdentityToken+`"}}}`)
		var calls []helperCall
		store := ociregistry.NewDockerCredentialStoreWithDeps(
			credentialSources(map[string]string{"DOCKER_CONFIG": dir}, "", "", nil, &calls))

		// when
		credential, err := store.Lookup(t.Context(), "registry.example.com")

		// then
		require.NoError(t, err)
		assert.Equal(t, fixtureIdentityToken, credential.IdentityToken)
	})

	t.Run("should fall back to .docker/config.json in the home directory", func(t *testing.T) {
		t.Parallel()

		// given
		home := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(home, ".docker"), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(home, ".docker", "config.json"), []byte(
			`{"auths":{"registry.example.com":{"username":"`+fixtureUser+`","password":"`+fixturePassword+`"}}}`,
		), 0o600))
		var calls []helperCall
		store := ociregistry.NewDockerCredentialStoreWithDeps(credentialSources(nil, home, "", nil, &calls))

		// when
		credential, err := store.Lookup(t.Context(), "registry.example.com")

		// then
		require.NoError(t, err)
		assert.Equal(t, fixtureUser, credential.Username)
	})

	t.Run("should read DOCKER_AUTH_CONFIG before the configuration file", func(t *testing.T) {
		t.Parallel()

		// given
		dir := writeDockerConfig(t, `{"auths":{"registry.example.com":{"username":"from-file","password":"x"}}}`)
		env := map[string]string{
			"DOCKER_CONFIG": dir,
			"DOCKER_AUTH_CONFIG": `{"auths":{"registry.example.com":{"auth":"` +
				fixtureBasicAuth() + `"}}}`,
		}
		var calls []helperCall
		store := ociregistry.NewDockerCredentialStoreWithDeps(credentialSources(env, "", "", nil, &calls))

		// when
		credential, err := store.Lookup(t.Context(), "registry.example.com")

		// then
		require.NoError(t, err)
		assert.Equal(t, fixtureUser, credential.Username)
	})

	t.Run("should ask the credential helper named for the host", func(t *testing.T) {
		t.Parallel()

		// given
		dir := writeDockerConfig(t, `{"credsStore":"other","credHelpers":{"registry.example.com":"fixture"}}`)
		var calls []helperCall
		store := ociregistry.NewDockerCredentialStoreWithDeps(credentialSources(
			map[string]string{"DOCKER_CONFIG": dir}, "",
			`{"Username":"`+fixtureUser+`","Secret":"`+fixturePassword+`"}`, nil, &calls,
		))

		// when
		credential, err := store.Lookup(t.Context(), "registry.example.com")

		// then
		require.NoError(t, err)
		assert.Equal(t, ociregistry.Credential{Username: fixtureUser, Password: fixturePassword}, credential)
		assert.Equal(t, []helperCall{{helper: "fixture", serverURL: "registry.example.com"}}, calls)
	})

	t.Run("should read an identity token from a helper reporting the token username", func(t *testing.T) {
		t.Parallel()

		// given
		dir := writeDockerConfig(t, `{"credsStore":"fixture"}`)
		var calls []helperCall
		store := ociregistry.NewDockerCredentialStoreWithDeps(credentialSources(
			map[string]string{"DOCKER_CONFIG": dir}, "",
			`{"Username":"<token>","Secret":"`+fixtureIdentityToken+`"}`, nil, &calls,
		))

		// when
		credential, err := store.Lookup(t.Context(), "registry.example.com")

		// then
		require.NoError(t, err)
		assert.Equal(t, ociregistry.Credential{IdentityToken: fixtureIdentityToken}, credential)
	})

	t.Run("should fall back to the auths entry when the helper holds nothing", func(t *testing.T) {
		t.Parallel()

		// given
		dir := writeDockerConfig(t, `{"credsStore":"fixture","auths":{"registry.example.com":{"auth":"`+
			fixtureBasicAuth()+`"}}}`)
		var calls []helperCall
		store := ociregistry.NewDockerCredentialStoreWithDeps(credentialSources(
			map[string]string{"DOCKER_CONFIG": dir}, "",
			"credentials not found in native keychain", errors.New("exit status 1"), &calls,
		))

		// when
		credential, err := store.Lookup(t.Context(), "registry.example.com")

		// then
		require.NoError(t, err)
		assert.Equal(t, fixtureUser, credential.Username)
		assert.Len(t, calls, 1)
	})

	t.Run("should fail without quoting a helper that fails for another reason", func(t *testing.T) {
		t.Parallel()

		// given
		dir := writeDockerConfig(t, `{"credsStore":"fixture"}`)
		var calls []helperCall
		store := ociregistry.NewDockerCredentialStoreWithDeps(credentialSources(
			map[string]string{"DOCKER_CONFIG": dir}, "",
			"partial output "+fixturePassword, errors.New("exit status 2"), &calls,
		))

		// when
		_, err := store.Lookup(t.Context(), "registry.example.com")

		// then
		require.Error(t, err)
		assert.NotContains(t, err.Error(), fixturePassword)
	})

	t.Run("should refuse to run a helper whose name is not a bare word", func(t *testing.T) {
		t.Parallel()

		// given
		dir := writeDockerConfig(t, `{"credsStore":"../../tmp/fixture"}`)
		var calls []helperCall
		store := ociregistry.NewDockerCredentialStoreWithDeps(
			credentialSources(map[string]string{"DOCKER_CONFIG": dir}, "", "", nil, &calls))

		// when
		_, err := store.Lookup(t.Context(), "registry.example.com")

		// then
		require.ErrorIs(t, err, ociregistry.ErrInvalidDockerConfig)
		assert.Empty(t, calls)
	})

	t.Run("should find Docker Hub's credential under its historical address", func(t *testing.T) {
		t.Parallel()

		// given
		dir := writeDockerConfig(t, `{"auths":{"https://index.docker.io/v1/":{"auth":"`+
			fixtureBasicAuth()+`"}}}`)
		var calls []helperCall
		store := ociregistry.NewDockerCredentialStoreWithDeps(
			credentialSources(map[string]string{"DOCKER_CONFIG": dir}, "", "", nil, &calls))

		// when
		credential, err := store.Lookup(t.Context(), "registry-1.docker.io")

		// then
		require.NoError(t, err)
		assert.Equal(t, fixtureUser, credential.Username)
	})

	t.Run("should answer no credential when there is no configuration", func(t *testing.T) {
		t.Parallel()

		// given
		var calls []helperCall
		store := ociregistry.NewDockerCredentialStoreWithDeps(credentialSources(nil, t.TempDir(), "", nil, &calls))

		// when
		credential, err := store.Lookup(t.Context(), "registry.example.com")

		// then
		require.NoError(t, err)
		assert.True(t, credential.IsZero())
	})

	t.Run("should fail when the configuration file is not JSON", func(t *testing.T) {
		t.Parallel()

		// given
		dir := writeDockerConfig(t, "not json")
		var calls []helperCall
		store := ociregistry.NewDockerCredentialStoreWithDeps(
			credentialSources(map[string]string{"DOCKER_CONFIG": dir}, "", "", nil, &calls))

		// when
		_, err := store.Lookup(t.Context(), "registry.example.com")

		// then
		require.ErrorIs(t, err, ociregistry.ErrInvalidDockerConfig)
	})
}

// TestRunCredentialHelper is not parallel: it puts a fake helper on PATH with t.Setenv.
func TestRunCredentialHelper(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake helper is a shell script")
	}

	t.Run("should run docker-credential-<helper> get with the server on standard input", func(t *testing.T) {
		// given
		dir := t.TempDir()
		script := "#!/bin/sh\nread server\nprintf '{\"Username\":\"%s\",\"Secret\":\"" + fixturePassword +
			"\"}' \"$server\"\n"
		// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
		require.NoError(t, os.WriteFile(filepath.Join(dir, "docker-credential-fixture"), []byte(script), 0o700))
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

		// when
		output, err := ociregistry.RunCredentialHelper(t.Context(), "fixture", "registry.example.com")

		// then
		require.NoError(t, err)
		assert.JSONEq(t, `{"Username":"registry.example.com","Secret":"`+fixturePassword+`"}`, string(output))
	})
}
