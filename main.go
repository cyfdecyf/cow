package main

import (
	"net/http"
)

func handleProxyReq(w http.ResponseWriter, r *http.Request) {
	debug.Println(r)
}

func main() {
	http.HandleFunc("/", handleProxyReq)
	http.ListenAndServe(":9000", nil)
}
