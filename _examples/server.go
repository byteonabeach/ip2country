package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/byteonabeach/ip2country"
)

type contextKey string

const countryCodeKey = contextKey("countryCode")

func createCountryMiddleware(db ip2country.IPCountryLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getIPAddress(r)

			code, err := db.GetCountryCodeWithContext(r.Context(), ip)
			if err == nil {
				ctx := context.WithValue(r.Context(), countryCodeKey, code)
				r = r.WithContext(ctx)
			}

			next.ServeHTTP(w, r)
		})
	}
}

func someHandler(w http.ResponseWriter, r *http.Request) {
	code, ok := r.Context().Value(countryCodeKey).(string)

	if !ok {
		fmt.Fprintln(w, "Welcome! Your country could not be determined.")
		return
	}

	fmt.Fprintf(w, "Welcome! It looks like you are visiting from country: %s\n", code)
}

func getIPAddress(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}

	if realIP := r.Header.Get("X-Real-Ip"); realIP != "" {
		return realIP
	}

	ip, _, _ := net.SplitHostPort(r.RemoteAddr)

	return ip
}

func main() {
	db := ip2country.NewIPCountryDB("ip_to_country.csv")
	countryMiddleware := createCountryMiddleware(db)

	http.Handle("/", countryMiddleware(http.HandlerFunc(someHandler))

	log.Println("Server starting...")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
