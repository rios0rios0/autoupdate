package ociregistry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// challenge is a parsed `WWW-Authenticate` header: the scheme the registry asks for and the
// parameters it gives with it, keys lowercased.
type challenge struct {
	scheme string
	params map[string]string
}

// parseChallenge reads a `WWW-Authenticate` header such as
// `Bearer realm="https://auth.example.com/token",service="registry.example.com"`.
//
// Quoted values may hold commas -- a scope naming two actions is
// `repository:team/api:pull,push` -- so the parameters are read value by value rather than
// split on commas.
func parseChallenge(header string) challenge {
	scheme, rest, _ := strings.Cut(strings.TrimSpace(header), " ")
	parsed := challenge{scheme: strings.ToLower(scheme), params: make(map[string]string)}

	for {
		rest = strings.TrimLeft(rest, " \t,")
		key, after, found := strings.Cut(rest, "=")
		if !found {
			return parsed
		}

		var value string
		if quoted, ok := strings.CutPrefix(after, `"`); ok {
			value, rest = readQuoted(quoted)
		} else {
			value, rest, _ = strings.Cut(after, ",")
		}
		parsed.params[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
}

// readQuoted reads a quoted string whose opening quote has been consumed, honouring
// backslash escapes, and returns it with whatever follows the closing quote.
func readQuoted(text string) (string, string) {
	var value strings.Builder
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\\':
			if i+1 < len(text) {
				i++
				value.WriteByte(text[i])
			}
		case '"':
			return value.String(), text[i+1:]
		default:
			value.WriteByte(text[i])
		}
	}
	return value.String(), ""
}

// fetchToken exchanges what the registry's token service accepts for an access token.
//
// An identity token -- the refresh token `docker login` stores for a registry that issues
// one -- is posted as an OAuth2 refresh grant. Anything else is a GET, with the username and
// password as basic authentication when there are any and anonymously otherwise, which is
// how a public image is read. The token service must be HTTPS whatever the registry says:
// the credential is sent to it.
func (r *RegistryRepository) fetchToken(
	ctx context.Context, host string, ch challenge, credential Credential, defaultScope string,
) (string, error) {
	realm, err := url.Parse(ch.params["realm"])
	if err != nil || realm.Scheme != "https" || realm.Host == "" {
		return "", fmt.Errorf("%w: %s names a token service that is not an HTTPS URL", ErrUnauthorized, host)
	}

	scope := ch.params["scope"]
	if scope == "" {
		scope = defaultScope
	}

	req, err := tokenRequest(ctx, realm, ch.params["service"], scope, credential)
	if err != nil {
		return "", fmt.Errorf("failed to build the token request for %s: %w", host, err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to reach the token service of %s: %w", host, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: the token service of %s answered %d", ErrUnauthorized, host, resp.StatusCode)
	}

	var answer struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if decodeErr := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&answer); decodeErr != nil {
		return "", fmt.Errorf("%w: the token service of %s answered something that is not a token: %w",
			ErrUnexpectedResponse, host, decodeErr)
	}

	token := answer.Token
	if token == "" {
		token = answer.AccessToken
	}
	if token == "" {
		return "", fmt.Errorf("%w: the token service of %s answered no token", ErrUnauthorized, host)
	}
	return token, nil
}

// tokenRequest builds the request a token service is asked with.
func tokenRequest(
	ctx context.Context, realm *url.URL, service, scope string, credential Credential,
) (*http.Request, error) {
	if credential.IdentityToken != "" {
		form := url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {credential.IdentityToken},
			"scope":         {scope},
			"client_id":     {clientName},
		}
		if service != "" {
			form.Set("service", service)
		}

		req, err := http.NewRequestWithContext(
			ctx, http.MethodPost, realm.String(), strings.NewReader(form.Encode()),
		)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", clientName)
		return req, nil
	}

	query := realm.Query()
	if service != "" {
		query.Set("service", service)
	}
	for one := range strings.FieldsSeq(scope) {
		query.Add("scope", one)
	}
	target := *realm
	target.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", clientName)
	if credential.Username != "" {
		req.SetBasicAuth(credential.Username, credential.Password)
	}
	return req, nil
}

// basicAuthorization renders the `Authorization` header value of basic authentication.
func basicAuthorization(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}
