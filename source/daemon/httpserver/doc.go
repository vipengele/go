// Package httpserver holds the HTTP building blocks of a daemon: the JSON error envelope written by
// Error and the request body decoder DecodeJSON, which rejects a wrong media type, an unknown field,
// trailing data and an oversize body with a *RequestError that names the status and code to answer.
package httpserver
