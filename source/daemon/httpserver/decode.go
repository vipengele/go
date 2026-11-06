package httpserver

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

// RequestError is a request the server rejects: Status is the HTTP status to answer, Code the
// machine-readable error code and Message the text for the caller. It is the error DecodeJSON
// returns.
type RequestError struct {
	Status  int
	Code    string
	Message string
}

func (e *RequestError) Error() string { return e.Code + ": " + e.Message }

// DecodeJSON decodes the body of r into v. The Content-Type must be application/json or a
// structured-syntax "+json" type, whatever its parameters. The body must be one JSON value with no
// field v lacks and nothing after it. Every failure is a *RequestError:
//
//   - 415 unsupported_media_type: the Content-Type is missing, malformed or not JSON.
//   - 413 body_too_large: reading the body hit the limit of an http.MaxBytesReader.
//   - 400 bad_request: the body is malformed, has an unknown field or has trailing data.
func DecodeJSON(r *http.Request, v any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !isJSON(mediaType) {
		return &RequestError{
			Status:  http.StatusUnsupportedMediaType,
			Code:    "unsupported_media_type",
			Message: "Content-Type must be application/json",
		}
	}

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return bodyError(err, "request body is not valid JSON for this request: "+err.Error())
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("unexpected data after the JSON value")
		}
		return bodyError(err, "request body has data after the JSON value")
	}
	return nil
}

func isJSON(mediaType string) bool {
	return mediaType == "application/json" ||
		(strings.HasSuffix(mediaType, "+json") && strings.Count(mediaType, "/") == 1)
}

func bodyError(err error, message string) *RequestError {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return &RequestError{Status: http.StatusRequestEntityTooLarge, Code: "body_too_large", Message: "request body is too large"}
	}
	return &RequestError{Status: http.StatusBadRequest, Code: "bad_request", Message: message}
}
