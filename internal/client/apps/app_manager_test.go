package apps

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReverseDomainNameFromURL(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		expectedID string
	}{
		{
			name:       "standard github url",
			url:        "https://github.com/OpenMined/pingpong",
			expectedID: "com.github.openmined.pingpong",
		},
		{
			name:       "standard github url 2",
			url:        "https://github.com/madhavajay/youtube-wrapped",
			expectedID: "com.github.madhavajay.youtube-wrapped",
		},
		{
			name:       "standard gitlab url",
			url:        "https://gitlab.com/cznic/sqlite",
			expectedID: "com.gitlab.cznic.sqlite",
		},
		{
			name:       "invalid url",
			url:        "",
			expectedID: "",
		},
		{
			name:       "invalid url - only scheme",
			url:        "http://",
			expectedID: "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := url.Parse(test.url)

			id := appIDFromURL(parsed)
			require.NoError(t, err)
			assert.Equal(t, test.expectedID, id)
		})
	}
}
