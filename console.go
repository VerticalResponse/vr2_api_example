package main

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

type param struct {
	Name        string
	Placeholder string
}

// Every card is a GET. This app is published and runs against a live
// environment with somebody's real account behind the token, so a create button
// here would leave rows in that account because a stranger pressed it.
type card struct {
	Key     string
	Path    string
	Summary string
	Bearer  bool
	Param   *param
	Index   bool
}

func (c card) Shown() string { return "/api/v1" + c.Path }

func (c card) returnTo() string {
	if c.Index {
		return "/"
	}
	return "/console#card-" + c.Key
}

var aliveCard = card{
	Key:     "is_alive",
	Path:    "/status/is_alive",
	Summary: "Answers whatever you send it, so it works before you have a token. If this answers and the console does not, the problem is the token rather than the routing.",
	Index:   true,
}

var cards = []card{
	{
		Key:     "contacts",
		Path:    "/contacts",
		Summary: "The first call that needs the token. Collections answer with a url, a count of what came back, and items.",
		Bearer:  true,
	},
	{
		Key:     "contact",
		Path:    "/contacts/{id}",
		Summary: "One contact, by the id of any row in the collection above. A single resource is a different envelope from a collection: attributes and links rather than count and items.",
		Bearer:  true,
		Param:   &param{Name: "id", Placeholder: "contact id"},
	},
	{
		Key:     "lists",
		Path:    "/lists",
		Summary: "A different resource in the same collection envelope. Once you can read one collection you can read all of them.",
		Bearer:  true,
	},
	{
		Key:     "list_contacts",
		Path:    "/lists/{id}/contacts",
		Summary: "The contacts on one list, paginated like any other collection. An id that belongs to no list answers 404 rather than an empty collection, which is what tells asking for the wrong list apart from asking for an empty one.",
		Bearer:  true,
		Param:   &param{Name: "id", Placeholder: "list id"},
	},
}

func cardByKey(key string) (card, bool) {
	if key == aliveCard.Key {
		return aliveCard, true
	}
	for _, c := range cards {
		if c.Key == key {
			return c, true
		}
	}
	return card{}, false
}

func (srv *server) console(w http.ResponseWriter, r *http.Request) {
	s := srv.store.get(w, r)
	if s.token().AccessToken == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	view := consolePage{
		page:  srv.base(s, "Console", r),
		Token: s.token(),
	}
	for _, c := range cards {
		res, ran := s.result(c.Key)
		view.Cards = append(view.Cards, consoleCard{card: c, Result: res, Ran: ran})
	}
	render(w, "console", http.StatusOK, view)
}

// Results are kept on the session so that running a second card does not blank
// the answer the first one gave.
func (srv *server) call(w http.ResponseWriter, r *http.Request) {
	s := srv.store.get(w, r)

	c, ok := cardByKey(r.PathValue("card"))
	if !ok {
		http.NotFound(w, r)
		return
	}

	var bearer string
	if c.Bearer {
		bearer = s.token().AccessToken
		if bearer == "" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}

	path := c.Path
	var input string
	if c.Param != nil {
		// The field is marked required, so an empty value did not come from
		// the form on the page.
		input = strings.TrimSpace(r.PostFormValue(c.Param.Name))
		if input == "" {
			http.Redirect(w, r, c.returnTo(), http.StatusSeeOther)
			return
		}
		path = strings.ReplaceAll(path, "{"+c.Param.Name+"}", url.PathEscape(input))
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	res, err := srv.client.get(ctx, srv.cfg.apiURL(path), bearer)
	if err != nil {
		s.setResult(c.Key, cardResult{Input: input, Failure: err.Error()})
	} else {
		s.setResult(c.Key, cardResult{
			Input:    input,
			Status:   res.Status,
			Duration: res.Duration,
			Body:     prettyJSON(res.Body),
		})
	}

	http.Redirect(w, r, c.returnTo(), http.StatusSeeOther)
}
