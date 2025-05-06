package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/client/config"
	"github.com/openmined/syftbox/internal/client/datasitemgr"
	"github.com/openmined/syftbox/internal/syftsdk"
)

const (
	ErrCodeProvisionFailed = "ERR_DATASITE_PROVISION_FAILED"
	ErrRequestEmailCode    = "ERR_REQUEST_EMAIL_CODE"
	ErrCodeVerifyEmailCode = "ERR_VERIFY_EMAIL_CODE"
)

type InitHandler struct {
	mgr             *datasitemgr.DatasiteManger
	controlPlaneURL string
}

func NewInitHandler(mgr *datasitemgr.DatasiteManger, controlPlaneURL string) *InitHandler {
	return &InitHandler{
		mgr:             mgr,
		controlPlaneURL: controlPlaneURL,
	}
}

// @Summary		Get token
// @Description	Request an email validation token from the syftbox server
// @Tags		init
// @Produce		json
// @Param		email		query		string	true	"Email"			Format(email)
// @Param		server_url	query		string	true	"Server URL"	Format(url)
// @Success		200			{object}	ControlPlaneResponse
// @Failure		400			{object}	ControlPlaneError
// @Router		/v1/init/token [get]
func (h *InitHandler) GetToken(c *gin.Context) {
	var req GetTokenRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, &ControlPlaneError{
			ErrorCode: ErrCodeBadRequest,
			Error:     err.Error(),
		})
		return
	}

	err := syftsdk.VerifyEmail(c.Request.Context(), req.ServerURL, req.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrRequestEmailCode,
			Error:     err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &ControlPlaneResponse{
		Code: CodeOk,
	})
}

// @Summary		Initialize the client
// @Description	Initialize the client with the given configuration
// @Tags		init
// @Accept		json
// @Produce		json
// @Param		request	body		InitDatasiteRequest	true	"Initialize request"
// @Success		200		{object}	ControlPlaneResponse
// @Failure		400		{object}	ControlPlaneError
// @Failure		500		{object}	ControlPlaneError
// @Router		/v1/init/datasite [post]
func (h *InitHandler) InitDatasite(c *gin.Context) {
	var req InitDatasiteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, &ControlPlaneError{
			ErrorCode: ErrCodeBadRequest,
			Error:     err.Error(),
		})
		return
	}

	resp, err := syftsdk.VerifyEmailCode(c.Request.Context(), req.ServerURL, &syftsdk.VerifyEmailCodeRequest{
		Email: req.Email,
		Code:  req.EmailToken,
	})
	if err != nil {
		c.JSON(http.StatusForbidden, &ControlPlaneError{
			ErrorCode: ErrCodeVerifyEmailCode,
			Error:     err.Error(),
		})
		return
	}

	// save config
	cfg := config.Config{
		DataDir:      req.DataDir,
		ServerURL:    req.ServerURL,
		ClientURL:    h.controlPlaneURL,
		Email:        req.Email,
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		AppsEnabled:  true,
	}

	if err := h.mgr.Provision(&cfg); err != nil {
		c.JSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeProvisionFailed,
			Error:     err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &ControlPlaneResponse{
		Code: CodeOk,
	})
}
