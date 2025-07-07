//go:build sonic

package syftsdk

import (
	"io"

	"github.com/bytedance/sonic"
)

// for imroc/req
var jsonMarshal = sonic.Marshal
var jsonUmarshal = sonic.Unmarshal

// for go resty
func jsonEncoder(w io.Writer, v any) error {
	return sonic.ConfigDefault.NewEncoder(w).Encode(v)
}

func jsonEncoder(r io.Reader, v any) error {
	return sonic.ConfigDefault.NewDecoder(r).Decode(v)
}
