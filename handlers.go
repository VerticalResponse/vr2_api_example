package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

type server struct {
	cfg    config
	store  *store
	client *client
}

func (srv *server) routes() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", srv.index)
	mux.HandleFunc("POST /connect", srv.connect)
	mux.HandleFunc("GET /callback", srv.callback)

	mux.HandleFunc("GET /console", srv.console)
	mux.HandleFunc("POST /call/{card}", srv.call)

	mux.HandleFunc("POST /refresh", srv.refresh)
	mux.HandleFunc("GET /revoke", srv.revokeConfirm)
	mux.HandleFunc("POST /revoke", srv.revoke)

	mux.Handle("GET /static/", http.FileServer(http.FS(assets)))
	return mux
}

// Notices are looked up rather than echoed so nothing from the query string
// reaches a page.
var notices = map[string]string{
	"refreshed": "Refreshed. The access token is new and so is the refresh token — the old one is already dead.",
	"revoked":   "Authorization revoked. Try Refresh now: it should fail with a 400 invalid_grant.",
}

func (srv *server) index(w http.ResponseWriter, r *http.Request) {
	s := srv.store.get(w, r)
	id, _ := s.client()
	res, ran := s.result(aliveCard.Key)
	render(w, "index", http.StatusOK, indexPage{
		page:     srv.base(s, "Connect", r),
		ClientID: id,
		Card:     consoleCard{card: aliveCard, Result: res, Ran: ran},
	})
}

func (srv *server) connect(w http.ResponseWriter, r *http.Request) {
	s := srv.store.get(w, r)

	clientID := strings.TrimSpace(r.PostFormValue("client_id"))
	secret := strings.TrimSpace(r.PostFormValue("client_secret"))
	if clientID == "" || secret == "" {
		srv.problem(w, r, s, http.StatusBadRequest, "Both client_id and client_secret are needed to start the flow.", nil)
		return
	}
	s.setClient(clientID, secret)

	state, err := s.newState()
	if err != nil {
		srv.problem(w, r, s, http.StatusInternalServerError, "Could not generate a state value.", err)
		return
	}

	http.Redirect(w, r, consentURL(srv.cfg, clientID, state), http.StatusFound)
}

func (srv *server) callback(w http.ResponseWriter, r *http.Request) {
	s := srv.store.get(w, r)
	q := r.URL.Query()

	if !s.consumeState(q.Get("state")) {
		srv.problem(w, r, s, http.StatusBadRequest,
			"The state on the callback does not match the one this session generated. The response did not come from the redirect we started, so nothing in it can be trusted and the code will not be exchanged.", nil)
		return
	}

	// Denial and the pre-consent validation failures all come back the same
	// way, as ?error= on the redirect URI.
	if code := q.Get("error"); code != "" {
		meaning := tokenErrorMeaning[code]
		if code == "access_denied" {
			meaning = "The user declined to give this application access to their account."
		}
		srv.problem(w, r, s, http.StatusOK, "The provider refused before issuing a code.",
			&providerError{Status: http.StatusOK, Code: code, Meaning: meaning})
		return
	}

	code := q.Get("code")
	if code == "" {
		srv.problem(w, r, s, http.StatusBadRequest, "The callback carried neither a code nor an error.", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	tok, err := exchangeCode(ctx, srv.client, s, srv.cfg, code)
	if err != nil {
		srv.problem(w, r, s, http.StatusOK, "The authorization code could not be exchanged for a token.", err)
		return
	}
	s.setToken(tok)
	http.Redirect(w, r, "/console", http.StatusSeeOther)
}

func (srv *server) refresh(w http.ResponseWriter, r *http.Request) {
	s := srv.store.get(w, r)
	if s.token().RefreshToken == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	tok, err := refreshToken(ctx, srv.client, s, srv.cfg)
	if err != nil {
		srv.problem(w, r, s, http.StatusOK, "The refresh failed.", err)
		return
	}
	s.setToken(tok)
	http.Redirect(w, r, "/console?notice=refreshed", http.StatusSeeOther)
}

// Revoking is destructive, so it gets its own page with a form on it. No
// browser dialog: window.confirm blocks the page and there is no reason to
// reach for it here.
func (srv *server) revokeConfirm(w http.ResponseWriter, r *http.Request) {
	s := srv.store.get(w, r)
	if s.token().AccessToken == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	render(w, "revoke", http.StatusOK, srv.base(s, "Revoke", r))
}

func (srv *server) revoke(w http.ResponseWriter, r *http.Request) {
	s := srv.store.get(w, r)
	if s.token().AccessToken == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := revokeAuthorization(ctx, srv.client, s, srv.cfg); err != nil {
		srv.problem(w, r, s, http.StatusOK, "The revoke call failed.", err)
		return
	}
	s.markRevoked()
	http.Redirect(w, r, "/console?notice=revoked", http.StatusSeeOther)
}

func (srv *server) base(s *session, title string, r *http.Request) page {
	tok := s.token()
	return page{
		Title:     title,
		Config:    srv.cfg,
		Connected: tok.AccessToken != "",
		Revoked:   tok.Revoked,
		Expired:   tok.Expired(),
		Notice:    notices[r.URL.Query().Get("notice")],
	}
}

func (srv *server) problem(w http.ResponseWriter, r *http.Request, s *session, status int, summary string, err error) {
	var view problemPage

	var pe *providerError
	switch {
	case errors.As(err, &pe):
		view.Code = pe.Code
		view.Meaning = pe.Meaning
		view.Body = pe.Body
		view.Status = pe.Status
		view.Failures = pe.Failures
	case err != nil:
		view.Meaning = err.Error()
	}

	view.page = srv.base(s, "Problem", r)
	view.page.Problem = summary
	render(w, "problem", status, view)
}
