package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddrToURL(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
		err  bool
	}{
		{"addr-with-host-port", "localhost:8080", "http://localhost:8080", false},
		{"addr-with-ip-port", "0.0.0.0:8080", "http://0.0.0.0:8080", false},
		{"addr-with-only-port", ":8080", "http://0.0.0.0:8080", false},
		{"addr-with-only-host", "localhost:", "", true},
		{"addr-missing-host", "8080", "", true},
		{"addr-missing-port", "localhost", "", true},
		{"addr-with-http", "http://localhost:8080", "", true},
		{"empty", "", "", true},
	}
	for _, test := range tests {
		val, err := addrToURL(test.addr)
		if test.err {
			assert.Error(t, err, test.name)
		} else {
			assert.NoError(t, err)
			assert.Equal(t, test.want, val, test.name)
		}
	}
}
