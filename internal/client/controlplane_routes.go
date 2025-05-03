package client

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ulule/limiter/v3"
	"github.com/ulule/limiter/v3/drivers/store/memory"

	_ "github.com/openmined/syftbox/internal/client/docs"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	mgin "github.com/ulule/limiter/v3/drivers/middleware/gin"

	"github.com/openmined/syftbox/internal/client/datasitemgr"
	"github.com/openmined/syftbox/internal/client/handlers"
	"github.com/openmined/syftbox/internal/client/middleware"
	"github.com/openmined/syftbox/internal/version"
)

//	@title						SyftBox Control Plane API
//	@version					0.5.0
//	@description				HTTP API for interfacing with SyftBox
//	@BasePath					/
//	@securityDefinitions.apikey	APIToken
//	@in							header
//	@name						Authorization
//
// @license.name				Apache 2.0
// @license.url				http://www.apache.org/licenses/LICENSE-2.0.html

type RouteConfig struct {
	Auth    middleware.TokenAuthConfig
	Swagger bool
}

func SetupRoutes(datasiteMgr *datasitemgr.DatasiteManger, routeConfig *RouteConfig) http.Handler {
	r := gin.New()

	rateLimitStore := memory.NewStore()
	rateLimiter := limiter.New(rateLimitStore, limiter.Rate{
		Period: 1 * time.Second,
		Limit:  10,
	})

	// syncH := handlers.NewSyncHandler(datasiteMgr)
	// fsH := handlers.NewFsHandler(datasiteMgr)
	appH := handlers.NewAppHandler(datasiteMgr)
	initH := handlers.NewInitHandler(datasiteMgr)
	statusH := handlers.NewStatusHandler(datasiteMgr)
	workspaceH := handlers.NewWorkspaceHandler(datasiteMgr)

	r.Use(gin.Recovery())
	r.Use(middleware.CORS())
	r.Use(middleware.Gzip())
	r.Use(mgin.NewMiddleware(rateLimiter))

	r.GET("/", IndexHandler)

	// @Security APIToken
	v1 := r.Group("/v1")
	v1.Use(middleware.TokenAuth(routeConfig.Auth))
	{
		v1.GET("/status", statusH.Status)

		v1Init := v1.Group("/init")
		{
			v1Init.GET("/token", initH.GetToken)
			v1Init.POST("/datasite", initH.InitDatasite)
		}

		v1App := v1.Group("/app")
		{
			v1App.GET("/list", appH.List)
			v1App.POST("/install", appH.Install)
			v1App.DELETE("/uninstall", appH.Uninstall)
		}

		v1Workspace := v1.Group("/workspace")
		{
			v1Workspace.GET("/items", workspaceH.GetItems)
			v1Workspace.POST("/items", workspaceH.CreateItem)
			v1Workspace.DELETE("/items", workspaceH.DeleteItems)
			v1Workspace.POST("/items/move", workspaceH.MoveItems)
		}

		// v1Fs := v1.Group("/datasite")
		// {
		// 	v1Fs.GET("/ls", fsH.List)
		// 	v1Fs.POST("/rm", fsH.Remove)
		// 	v1Fs.POST("/cp", fsH.Copy)
		// 	v1Fs.POST("/mv", fsH.Move)
		// }

		// v1Sync := v1.Group("/sync")
		// {
		// 	v1Sync.GET("/status", syncH.Status)
		// 	v1Sync.GET("/events", syncH.Events)
		// 	v1Sync.GET("/now", syncH.Now)
		// }
	}

	if routeConfig.Swagger {
		slog.Info("swagger enabled")
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
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
