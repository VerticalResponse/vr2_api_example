package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// providerError is a refusal the provider reported, as opposed to a transport
// failure. The token endpoint answers with a flat {"error":"<code>"} body; the
// resource endpoints use a nested {"error":{"code":…,"message":…}} envelope
// instead, so the two cannot share a decoder.
type providerError struct {
	Status   int
	Code     string
	Meaning  string
	Body     string
	Failures map[string][]string
}

func (e *providerError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("provider returned %d", e.Status)
	}
	return fmt.Sprintf("provider returned %d %s", e.Status, e.Code)
}

// The registered RFC 6749 codes, used as a fallback when the provider sends no
// error_description. invalid_grant is deliberately broad in the spec, so it is
// the description rather than the code that tells you which of the several
// terminal conditions you hit.
var tokenErrorMeaning = map[string]string{
	"invalid_request":        "The request was malformed or missing a required parameter.",
	"invalid_client":         "The client_id is unknown, or the client_secret does not match it.",
	"invalid_grant":          "The code or refresh token is unknown, expired, revoked, or was issued to another client. Every one of those is terminal: start the consent flow again rather than retrying.",
	"unauthorized_client":    "This client is not allowed to use this grant or this endpoint.",
	"unsupported_grant_type": "grant_type was not client_credentials, authorization_code or refresh_token.",
}

func failure(res result) *providerError {
	var flat struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	e := &providerError{Status: res.Status, Body: prettyJSON(res.Body)}
	if err := json.Unmarshal(res.Body, &flat); err == nil {
		e.Code = flat.Error
		e.Meaning = flat.Description
		if e.Meaning == "" {
			e.Meaning = tokenErrorMeaning[flat.Error]
		}
	}
	return e
}

// consentURL is where the browser has to be sent. It cannot be fetched: it is a
// server-rendered page that needs the user's own logged-in session, and an
// unauthenticated visitor is bounced through login and back again.
func consentURL(cfg config, clientID, state string) string {
	q := url.Values{
		"client_id":     {clientID},
		"redirect_uri":  {cfg.Redirect},
		"response_type": {"code"},
		"state":         {state},
	}
	return cfg.consentURL() + "?" + q.Encode()
}

func exchangeCode(ctx context.Context, c *client, s *session, cfg config, code string) (tokenSet, error) {
	id, secret := s.client()
	return requestToken(ctx, c, cfg, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {id},
		"client_secret": {secret},
		"code":          {code},
		// Sent again on the exchange, and compared again. It has to be the
		// same string as the one that started the flow and the one registered
		// on the application.
		"redirect_uri": {cfg.Redirect},
	})
}

func refreshToken(ctx context.Context, c *client, s *session, cfg config) (tokenSet, error) {
	id, secret := s.client()
	return requestToken(ctx, c, cfg, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {id},
		"client_secret": {secret},
		"refresh_token": {s.token().RefreshToken},
	})
}

func requestToken(ctx context.Context, c *client, cfg config, values url.Values) (tokenSet, error) {
	res, err := c.postForm(ctx, cfg.tokenURL(), values)
	if err != nil {
		return tokenSet{}, err
	}
	if res.Status != http.StatusOK {
		return tokenSet{}, failure(res)
	}

	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(res.Body, &body); err != nil {
		return tokenSet{}, fmt.Errorf("reading token response: %w", err)
	}

	return tokenSet{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		TokenType:    body.TokenType,
		ExpiresAt:    time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}, nil
}

// revokeAuthorization stamps the authorization revoked and clears the stored
// codes and refresh tokens for it, so the next refresh comes back
// access_revoked.
//
// It answers 200 {"status":"ok"} even for a refresh token it has never seen.
// That is deliberate: the endpoint cannot be used to find out which tokens
// exist. The credentials go in the body rather than the query string so the
// secret stays out of URLs and access logs.
func revokeAuthorization(ctx context.Context, c *client, s *session, cfg config) error {
	id, secret := s.client()
	res, err := c.deleteForm(ctx, cfg.revokeURL(), url.Values{
		"client_id":     {id},
		"client_secret": {secret},
		"refresh_token": {s.token().RefreshToken},
	})
	if err != nil {
		return err
	}
	if res.Status != http.StatusOK {
		return failure(res)
	}
	return nil
}
