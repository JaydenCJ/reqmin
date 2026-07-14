// Command demo-server is a tiny loopback API used by the examples, the
// README quickstart, and scripts/smoke.sh. It imitates the kind of endpoint
// that makes header minimization worthwhile: of everything a browser sends,
// it only ever inspects the Authorization header and the user query param.
//
//	go run ./examples/demo-server            # listens on 127.0.0.1:8641
//	go run ./examples/demo-server -addr 127.0.0.1:0
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8641", "loopback address to listen on")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/orders", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer demo-token" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintln(w, `{"error":"missing or wrong bearer token"}`)
			return
		}
		if r.URL.Query().Get("user") != "42" {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintln(w, `{"error":"unknown user"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"ok":true,"orders":[{"id":9001,"total":"12.50"}]}`)
	})

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("listening on http://%s\n", ln.Addr())
	log.Fatal(http.Serve(ln, mux))
}
