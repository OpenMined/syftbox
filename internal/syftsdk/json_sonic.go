//go:build sonic

package syftsdk

import (
	"io"

	"github.com/bytedance/sonic"
)

var jsonMarshal = sonic.Marshal
var jsonUmarshal = sonic.Unmarshal

func jsonEncoder(w io.Writer, v any) error {
	return sonic.ConfigDefault.NewEncoder(w).Encode(v)
}

func jsonEncoder(r io.Reader, v any) error {
	return sonic.ConfigDefault.NewDecoder(r).Decode(v)
}
