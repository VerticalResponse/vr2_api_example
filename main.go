// Command vr2_api_example is a worked example of a third-party integration with
// the VerticalResponse API: the authorization-code flow end to end, and a few
// resource calls through the gateway with the token it produces.
//
// It keeps no state anywhere but memory. Restart it and every session, every
// credential and every token is gone.
package main

import (
	"errors"
	"log"
	"net/http"
	"time"
)

func main() {
	cfg := loadConfig()

	srv := &server{
		cfg:    cfg,
		store:  newStore(secureCookies(cfg.Redirect)),
		client: newClient(),
	}
	go srv.store.janitor(sweepInterval)

	log.Printf("gateway      %s", cfg.Gateway)
	log.Printf("provider     %s", cfg.Provider)
	log.Printf("redirect uri %s", cfg.Redirect)
	log.Printf("listening on %s", cfg.Addr)

	listener := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := listener.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
