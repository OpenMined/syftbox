package docs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSwaggerInfo_Basics(t *testing.T) {
	assert.NotNil(t, SwaggerInfo)
	assert.Equal(t, "SyftBox Control Plane API", SwaggerInfo.Title)
	assert.NotEmpty(t, SwaggerInfo.Version)
	assert.Equal(t, "/", SwaggerInfo.BasePath)
	assert.NotEmpty(t, SwaggerInfo.SwaggerTemplate)
}

