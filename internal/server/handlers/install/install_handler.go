package install

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed install.sh
var installShell string

//go:embed install.ps1
var installPowershell string

func ServeSH(c *gin.Context) {
	c.Header("Content-Type", "text/x-shellscript")
	c.Header("Content-Disposition", "attachment; filename=install.sh")
	c.String(http.StatusOK, installShell)
}

func ServePS1(c *gin.Context) {
	c.Header("Content-Type", "text/x-powershell")
	c.Header("Content-Disposition", "attachment; filename=install.ps1")
	c.String(http.StatusOK, installPowershell)
}
