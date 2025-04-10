package middleware

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// RateLimiterConfig configures the rate limiter middleware
type RateLimiterConfig struct {
	// RequestsPerSecond is the maximum number of requests allowed per second
	RequestsPerSecond float64
	// BurstSize is the maximum burst size allowed
	BurstSize int
	// ClientKeyFunc extracts a key from the request to identify the client
	ClientKeyFunc func(*gin.Context) string
}

// IPRateLimiter implements a rate limiter by client IP
type IPRateLimiter struct {
	mu          sync.RWMutex
	limiters    map[string]*rate.Limiter
	rs          rate.Limit
	burst       int
	clientKeyFn func(*gin.Context) string
}

// NewIPRateLimiter creates a new rate limiter for each client IP
func NewIPRateLimiter(config RateLimiterConfig) *IPRateLimiter {
	// Default client key function uses client IP
	clientKeyFn := config.ClientKeyFunc
	if clientKeyFn == nil {
		clientKeyFn = func(c *gin.Context) string {
			return c.ClientIP()
		}
	}

	return &IPRateLimiter{
		limiters:    make(map[string]*rate.Limiter),
		rs:          rate.Limit(config.RequestsPerSecond),
		burst:       config.BurstSize,
		clientKeyFn: clientKeyFn,
	}
}

// GetLimiter returns the rate limiter for the provided key
func (i *IPRateLimiter) GetLimiter(key string) *rate.Limiter {
	i.mu.RLock()
	limiter, exists := i.limiters[key]
	i.mu.RUnlock()

	if !exists {
		i.mu.Lock()
		// Double check if it was created while we were waiting for the lock
		limiter, exists = i.limiters[key]
		if !exists {
			limiter = rate.NewLimiter(i.rs, i.burst)
			i.limiters[key] = limiter
		}
		i.mu.Unlock()
	}

	return limiter
}

// RateLimit creates a middleware for rate limiting requests
func RateLimit(config RateLimiterConfig) gin.HandlerFunc {
	ipRateLimiter := NewIPRateLimiter(config)

	return func(c *gin.Context) {
		key := ipRateLimiter.clientKeyFn(c)
		limiter := ipRateLimiter.GetLimiter(key)

		if !limiter.Allow() {
			c.Header("Retry-After", "1")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many requests. Please try again later.",
			})
			return
		}

		c.Next()
	}
}
