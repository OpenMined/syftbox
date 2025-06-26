package explorer

import (
	"net/http"
	"net/http/httptest"
	"text/template"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestExplorerHandler_StatusCodes(t *testing.T) {
	// Test that serve404 returns 404 status code
	t.Run("serve404 returns 404 status", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		
		// Create a minimal handler with templates
		handler := &ExplorerHandler{
			tpl404: testTemplate404(),
		}
		
		// Create test context
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		
		// Call serve404
		handler.serve404(c, "test/file.html")
		
		// Assert 404 status code
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "404")
		assert.Contains(t, w.Body.String(), "test/file.html")
	})
	
	// Test that serveDir returns 200 status code
	t.Run("serveDir returns 200 status", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		
		// Create a minimal handler with templates
		handler := &ExplorerHandler{
			tplIndex: testTemplateIndex(),
		}
		
		// Create test context
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		
		// Create directory contents
		contents := &directoryContents{
			IsDir:   true,
			Files:   nil,
			Folders: []string{"folder1", "folder2"},
		}
		
		// Call serveDir
		handler.serveDir(c, "/test/", contents)
		
		// Assert 200 status code (default when no error)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Index of")
		assert.Contains(t, w.Body.String(), "folder1")
		assert.Contains(t, w.Body.String(), "folder2")
	})
}

// Helper to create test 404 template
func testTemplate404() *template.Template {
	tmpl := `<!DOCTYPE html>
<html>
<head><title>404 Not Found</title></head>
<body>
<h1>404 Not Found</h1>
<p>The requested file {{.Key}} was not found.</p>
</body>
</html>`
	return template.Must(template.New("404").Parse(tmpl))
}

// Helper to create test index template
func testTemplateIndex() *template.Template {
	tmpl := `<!DOCTYPE html>
<html>
<head><title>Index of {{.Path}}</title></head>
<body>
<h1>Index of {{.Path}}</h1>
<ul>
{{range .Folders}}<li>{{.}}/</li>{{end}}
{{range .Files}}<li>{{.Key}}</li>{{end}}
</ul>
</body>
</html>`
	return template.Must(template.New("index").Parse(tmpl))
}