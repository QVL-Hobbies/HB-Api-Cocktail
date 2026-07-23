package main

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
)

const bearerPrefix = "Bearer "

func requireLocalWrite(config Config, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackRequest(r) {
			writeError(w, http.StatusForbidden, "forbidden", "forbidden")
			return
		}
		if !hasValidWriteToken(r, config.LocalWriteToken) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		next(w, r)
	}
}

func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func hasValidWriteToken(r *http.Request, expected string) bool {
	if expected == "" {
		return false
	}
	header := r.Header.Get("Authorization")
	if len(header) <= len(bearerPrefix) || !strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return false
	}
	provided := header[len(bearerPrefix):]
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
