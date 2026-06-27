// Package middleware contains cross-cutting HTTP concerns: request IDs,
// structured access logging, panic recovery, authentication, and RBAC.
package middleware

import (
	"crypto/rand"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/gin-gonic/gin"

	"github.com/AmeerHamza2/web3-wallet-api/internal/auth"
)

// Context keys.
const (
	ctxRequestID = "request_id"
	ctxRole      = "role"
	ctxSubject   = "subject"
	headerReqID  = "X-Request-ID"
)

// RequestID assigns each request a correlation ID (honoring an inbound one) and
// echoes it back.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(headerReqID)
		if id == "" {
			var b [16]byte
			if _, err := rand.Read(b[:]); err == nil {
				id = hexutil.Encode(b[:])[2:]
			}
		}
		c.Set(ctxRequestID, id)
		c.Header(headerReqID, id)
		c.Next()
	}
}

// Logger emits one structured access-log line per request.
func Logger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Info("http request",
			slog.String("request_id", c.GetString(ctxRequestID)),
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", c.Writer.Status()),
			slog.Duration("latency", time.Since(start)),
			slog.String("client_ip", c.ClientIP()),
		)
	}
}

// Recovery converts a panic into a 500 without leaking internals to the client.
func Recovery(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("panic recovered",
					slog.String("request_id", c.GetString(ctxRequestID)),
					slog.Any("panic", r),
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": "internal server error",
				})
			}
		}()
		c.Next()
	}
}

// Authenticate validates the Bearer token and stores role/subject in context.
func Authenticate(verifier *auth.Issuer) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(h, prefix) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
			return
		}
		claims, err := verifier.Verify(strings.TrimPrefix(h, prefix))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}
		c.Set(ctxRole, claims.Role)
		c.Set(ctxSubject, claims.Subject)
		c.Next()
	}
}

// RequireRole enforces that the authenticated principal holds one of the roles.
// Admin is always permitted (role hierarchy: admin ⊇ user).
func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}
	return func(c *gin.Context) {
		role := c.GetString(ctxRole)
		if role == auth.RoleAdmin {
			c.Next()
			return
		}
		if _, ok := allowed[role]; !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			return
		}
		c.Next()
	}
}
