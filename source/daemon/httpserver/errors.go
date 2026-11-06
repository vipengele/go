package httpserver

import (
	"bytes"
	"net/http"
	"strconv"

	"go.vipengele.org/cli/spec"
)

// errorSchema is the schema version of the error envelope.
const errorSchema = 1

type errorPayload struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error answers with status and the JSON envelope {"error":{"code":code,"message":message}},
// written by spec.WriteJSON at schema 1. The response carries "Cache-Control: no-store" and
// "X-Content-Type-Options: nosniff", so no browser sniffs the body as HTML.
func Error(w http.ResponseWriter, status int, code, message string) {
	var body bytes.Buffer
	payload := errorPayload{Error: errorBody{Code: code, Message: message}}
	if err := spec.WriteJSON(&body, errorSchema, payload); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(body.Len()))
	w.WriteHeader(status)
	// nosemgrep: go.lang.security.audit.xss.no-direct-write-to-responsewriter.no-direct-write-to-responsewriter -- the body is JSON as application/json, sent with nosniff, never HTML
	_, _ = w.Write(body.Bytes())
}
