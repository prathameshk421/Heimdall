// Command pay serves the payments HTTP API from memory on -addr.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/aarush323/heimdall-fixture-pay/internal/app"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()
	srv := &http.Server{
		Addr:              *addr,
		Handler:           app.New(app.Config{}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("listening on %s", *addr)
	log.Fatal(srv.ListenAndServe())
}
