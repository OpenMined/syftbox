package middlewares

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/handlers/api"
	"github.com/ulule/limiter/v3"
	"github.com/ulule/limiter/v3/drivers/store/memory"

	mgin "github.com/ulule/limiter/v3/drivers/middleware/gin"
)

var rateLimitStore = memory.NewStore()

func RateLimiter(formattedRate string) gin.HandlerFunc {
	rate, err := limiter.NewRateFromFormatted(formattedRate)
	if err != nil {
		panic(err)
	}
	limiter := limiter.New(rateLimitStore, rate)
	return mgin.NewMiddleware(
		limiter,
		mgin.WithLimitReachedHandler(func(c *gin.Context) {
			c.PureJSON(http.StatusTooManyRequests, api.SyftAPIError{
				Code:    api.CodeRateLimited,
				Message: "rate limit exceeded",
			})
		}),
		mgin.WithErrorHandler(func(c *gin.Context, err error) {
			c.PureJSON(http.StatusInternalServerError, api.SyftAPIError{
				Code:    api.CodeInternalError,
				Message: err.Error(),
			})
		}),
	)
}
