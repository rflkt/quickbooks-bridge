// qbo-connect runs the OAuth 2.0 handshake once: it opens Intuit's consent
// page, waits for the redirect on localhost, and saves the tokens.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"time"

	"github.com/rflkt/quickbooks-bridge/internal/qbo"
)

func main() {
	cfg, err := qbo.ConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	redirect, err := url.Parse(cfg.RedirectURI)
	if err != nil {
		log.Fatal(err)
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		log.Fatal(err)
	}
	state := hex.EncodeToString(b)

	authURL := qbo.AuthorizeURL + "?" + url.Values{
		"client_id":     {cfg.ClientID},
		"response_type": {"code"},
		"scope":         {qbo.AccountingScope},
		"redirect_uri":  {cfg.RedirectURI},
		"state":         {state},
	}.Encode()

	done := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(redirect.Path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			done <- fmt.Errorf("state mismatch")
			return
		}
		if e := q.Get("error"); e != "" {
			http.Error(w, e, http.StatusBadRequest)
			done <- fmt.Errorf("authorization refused: %s", e)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		t, err := cfg.ExchangeCode(ctx, q.Get("code"), q.Get("realmId"))
		if err == nil {
			err = qbo.SaveTokens(cfg.TokenFile, t)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			done <- err
			return
		}
		fmt.Fprintf(w, "Connected to QuickBooks company %s. You can close this tab.", t.RealmID)
		done <- nil
	})

	srv := &http.Server{Addr: redirect.Host, Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			done <- err
		}
	}()

	fmt.Println("Opening Intuit's consent page. If it does not open, visit:")
	fmt.Println(authURL)
	_ = exec.Command("open", authURL).Start()

	if err := <-done; err != nil {
		log.Fatal(err)
	}
	_ = srv.Shutdown(context.Background())
	fmt.Println("Tokens saved to", cfg.TokenFile)
}
