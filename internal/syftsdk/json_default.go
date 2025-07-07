//go:build !sonic

package syftsdk

import (
	"io"

	"github.com/goccy/go-json"
)

// for imroc/req
var jsonMarshal = json.Marshal
var jsonUmarshal = json.Unmarshal

// for go resty
func jsonEncoder(w io.Writer, v any) error {
	return json.NewEncoder(w).Encode(v)
}

func jsonDecoder(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}
