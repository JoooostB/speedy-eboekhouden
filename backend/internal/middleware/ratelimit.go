package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// RateLimit returns middleware that limits requests per IP using Redis.
// maxRequests per window duration. Uses a sliding window counter.
// If maxRequests is 0, rate limiting is disabled (pass-through).
func RateLimit(redisClient *redis.Client, prefix string, maxRequests int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if maxRequests <= 0 {
			c.Next()
			return
		}
		ip := c.ClientIP()
		key := fmt.Sprintf("ratelimit:%s:%s", prefix, ip)
		ctx := context.Background()

		count, err := redisClient.Incr(ctx, key).Result()
		if err != nil {
			// If Redis is down, allow the request
			c.Next()
			return
		}

		if count == 1 {
			redisClient.Expire(ctx, key, window)
		}

		if count > int64(maxRequests) {
			c.Header("Retry-After", fmt.Sprintf("%d", int(window.Seconds())))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Te veel verzoeken. Probeer het later opnieuw.",
			})
			return
		}

		c.Next()
	}
}
