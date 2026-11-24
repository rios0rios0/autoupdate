package ociregistry

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	// dockerAuthConfigVariable holds a whole Docker configuration document, `auths` and
	// all, the way GitLab CI hands one to a job.
	dockerAuthConfigVariable = "DOCKER_AUTH_CONFIG"

	// dockerConfigVariable names the directory of the Docker configuration file, as it does
	// for the Docker CLI.
	dockerConfigVariable = "DOCKER_CONFIG"

	// helperTimeout bounds one call to a credential helper.
	helperTimeout = 30 * time.Second

	// helperNotFound is how the Docker credential helpers say they hold nothing for a
	// server, on standard output with a non-zero exit.
	helperNotFound = "credentials not found"

	// identityTokenUsername is the username a credential helper reports when its secret is
	// an identity token rather than a password.
	identityTokenUsername = "<token>"

	// dockerHubHost is the host the Docker configuration files Docker Hub's credentials
	// under, as `https://index.docker.io/v1/`.
	dockerHubHost = "index.docker.io"
)

// ErrInvalidDockerConfig is returned when a Docker configuration exists and cannot be read.
var ErrInvalidDockerConfig = errors.New("invalid Docker configuration")

// helperNamePattern is a credential helper's name: a bare word, because it becomes part of
// the name of a program to run.
var helperNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// HelperRunner runs `docker-credential-<helper> get` with serverURL on its standard input
// and returns its standard output.
type HelperRunner func(ctx context.Context, helper, serverURL string) ([]byte, error)

// DockerCredentialSources are where a DockerCredentialStore reads from.
type DockerCredentialSources struct {
	// LookupEnv reads an environment variable.
	LookupEnv func(key string) (string, bool)
	// HomeDir returns the home directory the default `.docker/config.json` lives in.
	HomeDir func() (string, error)
	// RunHelper runs a credential helper.
	RunHelper HelperRunner
}

// DockerCredentialStore finds a registry's credential where the Docker CLI keeps it.
//
// Two documents are read, in this order: the `DOCKER_AUTH_CONFIG` environment variable,
// which holds a whole configuration, and the configuration file, `config.json` in
// `$DOCKER_CONFIG` or else in `~/.docker`. In each, a credential helper named for the host
// (`credHelpers`), or the default store (`credsStore`), is asked first, and the host's
// `auths` entry is read when it holds nothing. Nothing is read from AutoUpdate's own
// configuration: a registry credential never belongs in a file that names which repositories
// to update.
type DockerCredentialStore struct {
	sources DockerCredentialSources
}

var _ CredentialStore = (*DockerCredentialStore)(nil)

// NewDockerCredentialStore returns a store over the process environment, the user's home
// directory and the credential helpers on PATH.
func NewDockerCredentialStore() *DockerCredentialStore {
	return NewDockerCredentialStoreWithDeps(DockerCredentialSources{
		LookupEnv: os.LookupEnv,
		HomeDir:   os.UserHomeDir,
		RunHelper: RunCredentialHelper,
	})
}

// NewDockerCredentialStoreWithDeps returns a store over the given sources.
func NewDockerCredentialStoreWithDeps(sources DockerCredentialSources) *DockerCredentialStore {
	return &DockerCredentialStore{sources: sources}
}

// Lookup implements CredentialStore.
func (s *DockerCredentialStore) Lookup(ctx context.Context, host string) (Credential, error) {
	documents, err := s.documents()
	if err != nil {
		return Credential{}, err
	}

	key := canonicalHost(host)
	for _, document := range documents {
		credential, found, lookupErr := s.fromDocument(ctx, document, key)
		if lookupErr != nil || found {
			return credential, lookupErr
		}
	}
	return Credential{}, nil
}

// dockerConfig is the part of a Docker configuration document that names credentials.
type dockerConfig struct {
	Auths       map[string]dockerAuth `json:"auths"`
	CredHelpers map[string]string     `json:"credHelpers"`
	CredsStore  string                `json:"credsStore"`
}

// dockerAuth is one `auths` entry.
type dockerAuth struct {
	Auth          string `json:"auth"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	IdentityToken string `json:"identitytoken"`
}

// documents returns the configuration documents to read, in the order they are consulted.
func (s *DockerCredentialStore) documents() ([]dockerConfig, error) {
	var documents []dockerConfig

	if raw, ok := s.sources.LookupEnv(dockerAuthConfigVariable); ok && strings.TrimSpace(raw) != "" {
		var document dockerConfig
		if err := json.Unmarshal([]byte(raw), &document); err != nil {
			return nil, fmt.Errorf("%w: %s is not a Docker configuration document: %w",
				ErrInvalidDockerConfig, dockerAuthConfigVariable, err)
		}
		documents = append(documents, document)
	}

	path := s.configFilePath()
	if path == "" {
		return documents, nil
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return documents, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the Docker configuration %s: %w", path, err)
	}

	var document dockerConfig
	if unmarshalErr := json.Unmarshal(data, &document); unmarshalErr != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrInvalidDockerConfig, path, unmarshalErr)
	}
	return append(documents, document), nil
}

// configFilePath returns the Docker configuration file's path, or "" when there is no
// directory to look in: no home directory is no configuration file, as for the Docker CLI.
func (s *DockerCredentialStore) configFilePath() string {
	if dir, ok := s.sources.LookupEnv(dockerConfigVariable); ok && strings.TrimSpace(dir) != "" {
		return filepath.Join(dir, "config.json")
	}

	home, err := s.sources.HomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".docker", "config.json")
}

// fromDocument finds the credential one document holds for host, already canonical.
func (s *DockerCredentialStore) fromDocument(
	ctx context.Context, document dockerConfig, host string,
) (Credential, bool, error) {
	if helper := document.helperFor(host); helper != "" {
		credential, found, err := s.fromHelper(ctx, helper, host)
		if err != nil || found {
			return credential, found, err
		}
	}

	for _, key := range slices.Sorted(maps.Keys(document.Auths)) {
		if canonicalHost(key) != host {
			continue
		}
		credential, err := document.Auths[key].credential()
		if err != nil {
			return Credential{}, false, fmt.Errorf("the auths entry for %s: %w", host, err)
		}
		if !credential.IsZero() {
			return credential, true, nil
		}
	}

	return Credential{}, false, nil
}

// helperFor returns the credential helper that holds host's credential: the one named for
// the host, else the default store, else none.
func (d dockerConfig) helperFor(host string) string {
	for _, key := range slices.Sorted(maps.Keys(d.CredHelpers)) {
		if canonicalHost(key) == host {
			return d.CredHelpers[key]
		}
	}
	return d.CredsStore
}

// credential decodes an `auths` entry. `auth` is base64 of `username:password`, and wins
// over the separate fields, as it does for the Docker CLI.
func (a dockerAuth) credential() (Credential, error) {
	credential := Credential{Username: a.Username, Password: a.Password, IdentityToken: a.IdentityToken}
	if a.Auth == "" {
		return credential, nil
	}

	decoded, err := base64.StdEncoding.DecodeString(a.Auth)
	if err != nil {
		return Credential{}, fmt.Errorf("%w: auth is not base64", ErrInvalidDockerConfig)
	}
	username, password, found := strings.Cut(string(decoded), ":")
	if !found {
		return Credential{}, fmt.Errorf("%w: auth does not hold a username and a password", ErrInvalidDockerConfig)
	}
	credential.Username, credential.Password = username, password
	return credential, nil
}

// fromHelper asks a credential helper for host's credential.
func (s *DockerCredentialStore) fromHelper(ctx context.Context, helper, host string) (Credential, bool, error) {
	if !helperNamePattern.MatchString(helper) {
		return Credential{}, false, fmt.Errorf("%w: %q is not a credential helper name",
			ErrInvalidDockerConfig, helper)
	}

	output, err := s.sources.RunHelper(ctx, helper, helperServerURL(host))
	if err != nil {
		if bytes.Contains(output, []byte(helperNotFound)) {
			return Credential{}, false, nil
		}
		// The output is not quoted: a helper that fails halfway can print part of what it
		// holds.
		return Credential{}, false, fmt.Errorf("the docker-credential-%s helper failed for %s: %w", helper, host, err)
	}

	var answer struct {
		Username string `json:"Username"`
		Secret   string `json:"Secret"`
	}
	if unmarshalErr := json.Unmarshal(output, &answer); unmarshalErr != nil {
		return Credential{}, false, fmt.Errorf("the docker-credential-%s helper answered something unreadable for %s",
			helper, host)
	}

	if answer.Username == identityTokenUsername {
		return Credential{IdentityToken: answer.Secret}, answer.Secret != "", nil //nolint:exhaustruct // a token alone
	}
	credential := Credential{Username: answer.Username, Password: answer.Secret, IdentityToken: ""}
	return credential, !credential.IsZero(), nil
}

// RunCredentialHelper runs `docker-credential-<helper> get` from PATH, as the Docker CLI
// does, and returns its standard output.
func RunCredentialHelper(ctx context.Context, helper, serverURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, helperTimeout)
	defer cancel()

	//nolint:gosec // G204: the operator's Docker configuration names the helper, as for the Docker CLI, and fromHelper checked it is a bare word
	cmd := exec.CommandContext(ctx, "docker-credential-"+helper, "get")
	cmd.Stdin = strings.NewReader(serverURL)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	return stdout.Bytes(), err
}

// canonicalHost reduces a configuration key or a registry name to the host it names: no
// scheme, no path, lowercase. The Docker configuration keys Docker Hub's credentials as
// `https://index.docker.io/v1/`, so the hosts Docker Hub answers on are all one key.
func canonicalHost(server string) string {
	host := strings.ToLower(strings.TrimSpace(server))
	if _, rest, found := strings.Cut(host, "://"); found {
		host = rest
	}
	host, _, _ = strings.Cut(host, "/")

	if host == "docker.io" || host == "registry-1.docker.io" {
		return dockerHubHost
	}
	return host
}

// helperServerURL is the server a credential helper is asked about: the host, except for
// Docker Hub, which `docker login` files under its historical address.
func helperServerURL(host string) string {
	if host == dockerHubHost {
		return "https://index.docker.io/v1/"
	}
	return host
}
