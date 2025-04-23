package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yashgorana/syftbox-go/internal/version"
)

//	@Summary		Get status
//	@Description	Returns the status of the service
//	@Tags			status
//	@Produce		json
//	@Success		200	{object}	StatusResponse
//	@Router			/status [get]
func Status(ctx *gin.Context) {
	ctx.PureJSON(http.StatusOK, &StatusResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Version:   version.Version,
		Revision:  version.Revision,
		BuildDate: version.BuildDate,
	})
}
