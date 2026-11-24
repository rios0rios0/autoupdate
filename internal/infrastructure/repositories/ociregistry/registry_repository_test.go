package ociregistry_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/ociregistry"
)

const (
	fixtureUser          = "fixture-user-placeholder"
	fixturePassword      = "fixture-password-placeholder"
	fixtureIdentityToken = "fixture-identity-token-placeholder"
	fixtureAccessToken   = "fixture-access-token-placeholder"
	tagsPath             = "/v2/team/api/tags/list"
	tokenPath            = "/token"
)

// credentialStoreStub answers every lookup with one credential, and records the hosts it
// was asked about.
type credentialStoreStub struct {
	mu         sync.Mutex
	credential ociregistry.Credential
	err        error
	lookups    []string
}

func (s *credentialStoreStub) Lookup(_ context.Context, host string) (ociregistry.Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lookups = append(s.lookups, host)
	return s.credential, s.err
}

func (s *credentialStoreStub) asked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.lookups...)
}

// fixtureCredentialStore holds the fixture username and password.
func fixtureCredentialStore() *credentialStoreStub {
	//nolint:exhaustruct // a store holding one credential
	return &credentialStoreStub{credential: ociregistry.Credential{Username: fixtureUser, Password: fixturePassword}}
}

// newRegistry starts a TLS server answering with handler, and returns a client trusting it
// and the host it listens on.
func newRegistry(
	t *testing.T, store ociregistry.CredentialStore, handler http.HandlerFunc,
) (*ociregistry.RegistryRepository, string) {
	t.Helper()

	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)

	return ociregistry.NewRegistryRepositoryWithDeps(server.Client(), store),
		strings.TrimPrefix(server.URL, "https://")
}

// writeTags answers a tag list.
func writeTags(w http.ResponseWriter, tags ...string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"name": "team/api", "tags": tags})
}

// bearerChallenge answers 401 with a challenge naming the token service on the same host.
func bearerChallenge(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(
		`Bearer realm="https://%s%s",service="%s",scope="repository:team/api:pull"`, r.Host, tokenPath, r.Host,
	))
	w.WriteHeader(http.StatusUnauthorized)
}

// tokenRegistry serves a token service that issues fixtureAccessToken, under tokenField,
// to a request grant accepts, and a tag list that answers only that token.
func tokenRegistry(grant func(r *http.Request) bool, tokenField string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			if !grant(r) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{tokenField: fixtureAccessToken})
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+fixtureAccessToken {
			bearerChallenge(w, r)
			return
		}
		writeTags(w, "1.0.0")
	}
}

// grantsBasicCredentials accepts the fixture username and password for the challenge's scope
// and service, as a token service is asked for them with a GET.
func grantsBasicCredentials(r *http.Request) bool {
	user, password, ok := r.BasicAuth()
	query := r.URL.Query()
	return ok && user == fixtureUser && password == fixturePassword &&
		query.Get("scope") == "repository:team/api:pull" && query.Get("service") == r.Host
}

// grantsRefreshToken accepts the fixture identity token posted as an OAuth2 refresh grant.
func grantsRefreshToken(r *http.Request) bool {
	return r.Method == http.MethodPost && r.ParseForm() == nil &&
		r.PostForm.Get("grant_type") == "refresh_token" &&
		r.PostForm.Get("refresh_token") == fixtureIdentityToken
}

// basicRegistry answers the tag list only with the fixture credential as basic authentication.
func basicRegistry(w http.ResponseWriter, r *http.Request) {
	if user, password, ok := r.BasicAuth(); !ok || user != fixtureUser || password != fixturePassword {
		w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	writeTags(w, "2.0.0")
}

// pagedRegistry answers the tag list in two pages.
func pagedRegistry(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("last") == "" {
		w.Header().Set("Link", `</v2/team/api/tags/list?last=0.0.2&n=2>; rel="next"`)
		writeTags(w, "0.0.1", "0.0.2")
		return
	}
	writeTags(w, "0.0.3")
}

func TestRegistryRepositoryListTagsAnonymously(t *testing.T) {
	t.Parallel()

	t.Run("should list a public repository's tags without asking for a credential", func(t *testing.T) {
		t.Parallel()

		// given
		store := &credentialStoreStub{}
		client, host := newRegistry(t, store, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != tagsPath {
				http.NotFound(w, r)
				return
			}
			writeTags(w, "0.0.3", "0.0.4")
		})

		// when
		tags, err := client.ListTags(t.Context(), host, "team/api")

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"0.0.3", "0.0.4"}, tags)
		assert.Empty(t, store.asked(), "a public registry never causes the credential store to be read")
	})

	t.Run("should follow the Link header across pages", func(t *testing.T) {
		t.Parallel()

		// given
		client, host := newRegistry(t, &credentialStoreStub{}, pagedRegistry)

		// when
		tags, err := client.ListTags(t.Context(), host, "team/api")

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"0.0.1", "0.0.2", "0.0.3"}, tags)
	})

	t.Run("should answer no tags when the registry holds no such repository", func(t *testing.T) {
		t.Parallel()

		// given
		client, host := newRegistry(t, &credentialStoreStub{}, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"code":"NAME_UNKNOWN"}]}`))
		})

		// when
		tags, err := client.ListTags(t.Context(), host, "team/api")

		// then
		require.NoError(t, err)
		assert.Empty(t, tags)
	})
}

func TestRegistryRepositoryListTagsAuthenticated(t *testing.T) {
	t.Parallel()

	t.Run("should fetch a bearer token with the stored username and password", func(t *testing.T) {
		t.Parallel()

		// given
		store := fixtureCredentialStore()
		client, host := newRegistry(t, store, tokenRegistry(grantsBasicCredentials, "token"))

		// when
		tags, err := client.ListTags(t.Context(), host, "team/api")

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"1.0.0"}, tags)
		assert.Equal(t, []string{host}, store.asked(), "the credential is looked up for the registry's own host")
	})

	t.Run("should exchange an identity token as an OAuth2 refresh grant", func(t *testing.T) {
		t.Parallel()

		// given
		//nolint:exhaustruct // a store holding an identity token alone
		store := &credentialStoreStub{credential: ociregistry.Credential{IdentityToken: fixtureIdentityToken}}
		client, host := newRegistry(t, store, tokenRegistry(grantsRefreshToken, "access_token"))

		// when
		tags, err := client.ListTags(t.Context(), host, "team/api")

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"1.0.0"}, tags)
	})

	t.Run("should send basic credentials when the registry asks for them", func(t *testing.T) {
		t.Parallel()

		// given
		client, host := newRegistry(t, fixtureCredentialStore(), basicRegistry)

		// when
		tags, err := client.ListTags(t.Context(), host, "team/api")

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"2.0.0"}, tags)
	})
}

func TestRegistryRepositoryListTagsRefusals(t *testing.T) {
	t.Parallel()

	t.Run("should fail when the registry refuses the token it issued", func(t *testing.T) {
		t.Parallel()

		// given -- a token service that issues a token the registry then refuses
		client, host := newRegistry(t, &credentialStoreStub{}, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == tokenPath {
				_ = json.NewEncoder(w).Encode(map[string]string{"token": fixtureAccessToken})
				return
			}
			bearerChallenge(w, r)
		})

		// when
		tags, err := client.ListTags(t.Context(), host, "team/api")

		// then
		require.ErrorIs(t, err, ociregistry.ErrUnauthorized)
		assert.Nil(t, tags)
		assert.NotContains(t, err.Error(), fixtureAccessToken)
	})

	t.Run("should fail when basic authentication is asked for and nothing is stored", func(t *testing.T) {
		t.Parallel()

		// given
		client, host := newRegistry(t, &credentialStoreStub{}, basicRegistry)

		// when
		_, err := client.ListTags(t.Context(), host, "team/api")

		// then
		require.ErrorIs(t, err, ociregistry.ErrUnauthorized)
	})

	t.Run("should refuse a token service that is not HTTPS", func(t *testing.T) {
		t.Parallel()

		// given
		var tokenRequests atomic.Int32
		client, host := newRegistry(t, fixtureCredentialStore(), func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == tokenPath {
				tokenRequests.Add(1)
				return
			}
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s%s"`, r.Host, tokenPath))
			w.WriteHeader(http.StatusUnauthorized)
		})

		// when
		_, err := client.ListTags(t.Context(), host, "team/api")

		// then -- the credential would travel in the clear
		require.ErrorIs(t, err, ociregistry.ErrUnauthorized)
		assert.Zero(t, tokenRequests.Load())
	})

	t.Run("should refuse a next page on another host", func(t *testing.T) {
		t.Parallel()

		// given
		client, host := newRegistry(t, &credentialStoreStub{}, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Link", `<https://elsewhere.example.net/v2/team/api/tags/list?last=1>; rel="next"`)
			writeTags(w, "1.0.0")
		})

		// when
		tags, err := client.ListTags(t.Context(), host, "team/api")

		// then -- the session's authorization would follow the request there
		require.ErrorIs(t, err, ociregistry.ErrUnexpectedResponse)
		assert.Nil(t, tags)
	})

	t.Run("should fail when the registry answers an unexpected status", func(t *testing.T) {
		t.Parallel()

		// given
		client, host := newRegistry(t, &credentialStoreStub{}, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})

		// when
		_, err := client.ListTags(t.Context(), host, "team/api")

		// then
		require.ErrorIs(t, err, ociregistry.ErrUnexpectedResponse)
	})

	t.Run("should fail when the credential store cannot be read", func(t *testing.T) {
		t.Parallel()

		// given
		storeErr := errors.New("fixture store failure")
		//nolint:exhaustruct // a store that only fails
		client, host := newRegistry(t, &credentialStoreStub{err: storeErr}, bearerChallenge)

		// when
		_, err := client.ListTags(t.Context(), host, "team/api")

		// then
		require.ErrorIs(t, err, storeErr)
	})
}
