package client

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ulule/limiter/v3"
	mgin "github.com/ulule/limiter/v3/drivers/middleware/gin"
	"github.com/ulule/limiter/v3/drivers/store/memory"

	_ "github.com/openmined/syftbox/internal/client/docs"

	"github.com/openmined/syftbox/internal/client/datasitemgr"
	"github.com/openmined/syftbox/internal/client/handlers"
	"github.com/openmined/syftbox/internal/client/middleware"
	"github.com/openmined/syftbox/internal/swaggerui"
	"github.com/openmined/syftbox/internal/version"
)

//	@title						SyftBox Control Plane API
//	@version					0.5.0
//	@description				HTTP API for interfacing with SyftBox
//	@BasePath					/
//	@securityDefinitions.apikey	APIToken
//	@in							header
//	@name						Authorization
//	@license.name				Apache 2.0
//	@license.url				http://www.apache.org/licenses/LICENSE-2.0.html

type RouteConfig struct {
	Auth            middleware.TokenAuthConfig
	ControlPlaneURL string
	Swagger         bool
}

func SetupRoutes(datasiteMgr *datasitemgr.DatasiteManager, routeConfig *RouteConfig) http.Handler {
	r := gin.New()

	rateLimitStore := memory.NewStore()
	rateLimiter := limiter.New(rateLimitStore, limiter.Rate{
		Period: 1 * time.Second,
		Limit:  10,
	})

	// syncH := handlers.NewSyncHandler(datasiteMgr)
	appH := handlers.NewAppHandler(datasiteMgr)
	initH := handlers.NewInitHandler(datasiteMgr, routeConfig.ControlPlaneURL)
	statusH := handlers.NewStatusHandler(datasiteMgr)
	workspaceH := handlers.NewWorkspaceHandler(datasiteMgr)
	logsH := handlers.NewLogsHandler(datasiteMgr)

	r.Use(gin.Recovery())
	r.Use(middleware.CORS())
	r.Use(middleware.Gzip())
	r.Use(mgin.NewMiddleware(rateLimiter))
	r.Use(middleware.Logger())

	r.GET("/", IndexHandler)

	//	@Security	APIToken
	v1 := r.Group("/v1")
	v1.Use(middleware.TokenAuth(routeConfig.Auth))
	{
		v1.GET("/status", statusH.Status)

		v1Init := v1.Group("/init")
		{
			v1Init.GET("/token", initH.GetToken)
			v1Init.POST("/datasite", initH.InitDatasite)
		}

		v1App := v1.Group("/apps")
		{
			v1App.GET("/", appH.List)
			v1App.GET("/:appId", appH.Get)
			v1App.POST("/:appId/start", appH.Start)
			v1App.POST("/:appId/stop", appH.Stop)
			v1App.POST("/", appH.Install)
			v1App.DELETE("/:appId", appH.Uninstall)
		}

		v1Workspace := v1.Group("/workspace")
		{
			v1Workspace.GET("/items", workspaceH.GetItems)
			v1Workspace.POST("/items", workspaceH.CreateItem)
			v1Workspace.DELETE("/items", workspaceH.DeleteItems)
			v1Workspace.POST("/items/move", workspaceH.MoveItems)
			v1Workspace.POST("/items/copy", workspaceH.CopyItems)
			v1Workspace.GET("/content", workspaceH.GetContent)
		}

		// Logs endpoint
		v1.GET("/logs", logsH.GetLogs)

		// v1Sync := v1.Group("/sync")
		// {
		// 	v1Sync.GET("/status", syncH.Status)
		// 	v1Sync.GET("/events", syncH.Events)
		// 	v1Sync.GET("/now", syncH.Now)
		// }
	}

	if routeConfig.Swagger {
		slog.Info("swagger enabled")
		swaggerui.SetupRoutes(r)
	}

	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "not found",
		})
	})

	r.NoMethod(func(c *gin.Context) {
		c.JSON(http.StatusMethodNotAllowed, gin.H{
			"error": "method not allowed",
		})
	})

	return r.Handler()
}

func init() {
	gin.SetMode(gin.ReleaseMode)
}

func IndexHandler(c *gin.Context) {
	c.JSON(http.StatusOK, version.Detailed())
}
