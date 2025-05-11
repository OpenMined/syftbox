package swaggerui

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/swaggo/swag/v2"
)

const swaggerUIHTML = `<!DOCTYPE html><html lang="en"><head><meta charset="utf-8" /><meta name="viewport" content="width=device-width, initial-scale=1" /><meta name="description" content="SwaggerUI" /><title>SwaggerUI</title><link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.21.0/swagger-ui.css" /><link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.21.0/index.css" /><link rel="icon" type="image/png" href="https://unpkg.com/swagger-ui-dist@5.21.0/favicon-32x32.png" sizes="32x32" /><link rel="icon" type="image/png" href="https://unpkg.com/swagger-ui-dist@5.21.0/favicon-16x16.png" sizes="16x16" /></head><body><div id="swagger-ui"></div><script src="https://unpkg.com/swagger-ui-dist@5.21.0/swagger-ui-bundle.js" crossorigin></script><script src="https://unpkg.com/swagger-ui-dist@5.21.0/swagger-ui-standalone-preset.js" crossorigin></script><script> window.onload = () => { window.ui = SwaggerUIBundle({ url: "/swagger/docs.json", dom_id: '#swagger-ui', deepLinking: true, presets: [ SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset ], plugins: [ SwaggerUIBundle.plugins.DownloadUrl ], layout: "StandaloneLayout" }); }; </script></body></html>`

func SetupRoutes(r *gin.Engine) {
	swagger := r.Group("/swagger")
	swagger.GET("/", SwaggerUIHandler)
	swagger.GET("/docs.json", APIDocsHandler)
}

func SwaggerUIHandler(c *gin.Context) {
	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, swaggerUIHTML)
}

func APIDocsHandler(c *gin.Context) {
	doc, err := swag.ReadDoc()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
	}
	c.String(http.StatusOK, doc)
}
