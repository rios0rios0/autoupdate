// Package ociregistry reads the tags a container registry holds through the OCI Distribution
// API, authenticating the way the Docker CLI does: anonymously first, then with the token or
// the credentials the registry's challenge asks for.
package ociregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

const (
	// requestTimeout bounds every request to a registry or its token service.
	requestTimeout = 30 * time.Second

	// maxTagPages caps how many pages of one tag list are read. The cap exists to stop a
	// registry that never stops answering; a list cut short only leaves out older tags,
	// which a check for a newer one does not need.
	maxTagPages = 100

	// maxResponseBytes bounds how much of one response is read.
	maxResponseBytes = 8 << 20

	// clientName identifies this program to registries and token services.
	clientName = "autoupdate"
)

var (
	// ErrUnauthorized is returned when the registry refuses the request and no credential
	// this program holds is accepted.
	ErrUnauthorized = errors.New("the registry refused the request")

	// ErrUnexpectedResponse is returned when the registry answers something that is not a
	// tag list.
	ErrUnexpectedResponse = errors.New("unexpected answer from the registry")
)

// nextLinkPattern reads the next page's address out of a `Link` header, which the OCI
// Distribution API uses to paginate a tag list.
var nextLinkPattern = regexp.MustCompile(`<([^>]+)>\s*;\s*rel="?next"?`)

// Credential is what a registry is authenticated with.
type Credential struct {
	Username string
	Password string

	// IdentityToken is an OAuth2 refresh token, which `docker login` stores for a registry
	// that issues one, and which is exchanged at the registry's token service for an access
	// token rather than sent as a password.
	IdentityToken string
}

// IsZero reports whether the credential holds nothing.
func (c Credential) IsZero() bool {
	return c == Credential{} //nolint:exhaustruct // the zero value is the comparison
}

// CredentialStore finds the credential stored for a registry host.
type CredentialStore interface {
	// Lookup returns the credential stored for host, the zero Credential when none is, and
	// an error only when the store itself could not be read.
	Lookup(ctx context.Context, host string) (Credential, error)
}

// RegistryRepository implements repositories.ImageRegistryRepository over HTTPS.
//
// Credentials are looked up only when a registry asks for them, so a public registry never
// causes the operator's credential store, or a credential helper, to be consulted, and a
// credential is only ever sent for the host it is stored under.
type RegistryRepository struct {
	client      *http.Client
	credentials CredentialStore
}

var _ repositories.ImageRegistryRepository = (*RegistryRepository)(nil)

// NewRegistryRepository returns a client that reads credentials from the Docker
// configuration, as the Docker CLI does.
func NewRegistryRepository() *RegistryRepository {
	return NewRegistryRepositoryWithDeps(&http.Client{Timeout: requestTimeout}, NewDockerCredentialStore())
}

// NewRegistryRepositoryWithDeps returns a client over the given HTTP client and credential
// store.
func NewRegistryRepositoryWithDeps(client *http.Client, credentials CredentialStore) *RegistryRepository {
	return &RegistryRepository{client: client, credentials: credentials}
}

// ListTags implements repositories.ImageRegistryRepository.
func (r *RegistryRepository) ListTags(ctx context.Context, registry, repository string) ([]string, error) {
	base := &url.URL{Scheme: "https", Host: registry, Path: "/"}
	page := base.JoinPath("v2", repository, "tags", "list")
	session := &registrySession{repository: r, host: registry, scope: "repository:" + repository + ":pull"}

	var tags []string
	for pageNumber := range maxTagPages {
		answer, err := session.fetchTagPage(ctx, page)
		if err != nil {
			return nil, err
		}
		if answer.missing {
			// The registry holds no repository by this name, which is an answer: it has
			// none of its tags.
			return tags, nil
		}

		tags = append(tags, answer.tags...)
		if answer.next == nil {
			return tags, nil
		}
		if pageNumber == maxTagPages-1 {
			logger.Warnf("Stopped reading the tags of %s/%s after %d pages", registry, repository, maxTagPages)
		}
		page = answer.next
	}

	return tags, nil
}

// tagPage is one page of a tag list.
type tagPage struct {
	tags    []string
	next    *url.URL
	missing bool
}

// registrySession holds what one tag list needs across its pages: the authorization the
// registry accepted, so each page after the first is asked for with it.
type registrySession struct {
	repository    *RegistryRepository
	host          string
	scope         string
	authorization string
	authenticated bool
}

// fetchTagPage asks for one page of the tag list, authenticating once when the registry asks
// for it.
func (s *registrySession) fetchTagPage(ctx context.Context, page *url.URL) (tagPage, error) {
	resp, err := s.get(ctx, page)
	if err != nil {
		return tagPage{}, err
	}

	if resp.StatusCode == http.StatusUnauthorized && !s.authenticated {
		asked := parseChallenge(resp.Header.Get("WWW-Authenticate"))
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		_ = resp.Body.Close()

		if authErr := s.authenticate(ctx, asked); authErr != nil {
			return tagPage{}, authErr
		}
		if resp, err = s.get(ctx, page); err != nil {
			return tagPage{}, err
		}
	}
	defer func() { _ = resp.Body.Close() }()

	return readTagPage(resp, page)
}

// get sends a GET for target with the session's authorization.
func (s *registrySession) get(ctx context.Context, target *url.URL) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build the request to %s: %w", s.host, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", clientName)
	if s.authorization != "" {
		req.Header.Set("Authorization", s.authorization)
	}

	resp, err := s.repository.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach %s: %w", s.host, err)
	}
	return resp, nil
}

// authenticate answers the registry's challenge, once per session: a registry that refuses
// what it asked for is not asked again.
func (s *registrySession) authenticate(ctx context.Context, ch challenge) error {
	s.authenticated = true

	credential, err := s.repository.credentials.Lookup(ctx, s.host)
	if err != nil {
		return fmt.Errorf("failed to read the credentials stored for %s: %w", s.host, err)
	}

	switch ch.scheme {
	case "bearer":
		token, tokenErr := s.repository.fetchToken(ctx, s.host, ch, credential, s.scope)
		if tokenErr != nil {
			return tokenErr
		}
		s.authorization = "Bearer " + token
	case "basic":
		if credential.Username == "" {
			return fmt.Errorf("%w: %s asks for credentials and none are stored for it", ErrUnauthorized, s.host)
		}
		s.authorization = basicAuthorization(credential.Username, credential.Password)
	default:
		return fmt.Errorf("%w: %s asks for %q authentication, which is not supported",
			ErrUnauthorized, s.host, ch.scheme)
	}

	return nil
}

// readTagPage reads one answer to a tag list request.
func readTagPage(resp *http.Response, page *url.URL) (tagPage, error) {
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return tagPage{missing: true}, nil //nolint:exhaustruct // a missing repository has no tags
	case http.StatusUnauthorized, http.StatusForbidden:
		return tagPage{}, fmt.Errorf("%w: %s answered %d", ErrUnauthorized, page.Host, resp.StatusCode)
	default:
		return tagPage{}, fmt.Errorf("%w: %s answered %d", ErrUnexpectedResponse, page.Host, resp.StatusCode)
	}

	var list struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&list); err != nil {
		return tagPage{}, fmt.Errorf("%w: %s answered a tag list that could not be read: %w",
			ErrUnexpectedResponse, page.Host, err)
	}

	next, err := nextPage(page, resp.Header.Get("Link"))
	if err != nil {
		return tagPage{}, err
	}

	return tagPage{tags: list.Tags, next: next, missing: false}, nil
}

// nextPage resolves the `Link` header's next page against the current one. A next page on
// another host is refused: the session's authorization would be sent there with it.
func nextPage(current *url.URL, header string) (*url.URL, error) {
	match := nextLinkPattern.FindStringSubmatch(header)
	if match == nil {
		return nil, nil //nolint:nilnil // no next page is not an error
	}

	reference, err := url.Parse(match[1])
	if err != nil {
		return nil, fmt.Errorf("%w: %s named a next page that is not a URL: %w",
			ErrUnexpectedResponse, current.Host, err)
	}

	next := current.ResolveReference(reference)
	if next.Scheme != current.Scheme || !strings.EqualFold(next.Host, current.Host) {
		return nil, fmt.Errorf("%w: %s named a next page on %s",
			ErrUnexpectedResponse, current.Host, next.Host)
	}
	return next, nil
}
