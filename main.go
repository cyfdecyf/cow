package main

import (
	"fmt"
	"io"
	"net/http"
)

var client = &http.Client{}

func handleProxyReq(w http.ResponseWriter, r *http.Request) {
	info.Println("RequestURI:", r.RequestURI)
	debug.Printf("Request: %v\n", r)

	// TODO how to handle request header and cookie?
	req, err := http.NewRequest(r.Method, r.RequestURI, r.Body)
	if err != nil {
		info.Println(err)
		fmt.Fprintln(w, err)
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		info.Println(err)
		fmt.Fprintln(w, err)
		return
	}

	// TODO How to pass response header back to client?
	debug.Printf("Response: %v\n", resp)
	ct := resp.Header["Content-Type"]
	if ct != nil {
		w.Header().Set("Content-Type", ct[0])
	}

	_, err = io.Copy(w, resp.Body)
	if err != nil {
		info.Println(err)
	}
	resp.Body.Close()
}

func main() {
	http.HandleFunc("/", handleProxyReq)
	http.ListenAndServe(":9000", nil)
}
