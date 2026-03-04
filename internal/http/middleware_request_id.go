package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

const (
	requestIDHeader = "X-Request-ID"
	requestIDKey    = "request_id"
)

type contextRequestIDKey struct{}

func requestIDMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		requestID := strings.TrimSpace(c.Get(requestIDHeader))
		if requestID == "" {
			requestID = generateRequestID()
		}

		c.Locals(requestIDKey, requestID)
		c.Set(requestIDHeader, requestID)

		ctx := context.WithValue(c.Context(), contextRequestIDKey{}, requestID)
		c.SetUserContext(ctx)

		return c.Next()
	}
}

func requestIDFromContext(c *fiber.Ctx) string {
	if c == nil {
		return ""
	}

	if requestID, ok := c.Locals(requestIDKey).(string); ok {
		return strings.TrimSpace(requestID)
	}
	return ""
}

func generateRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return strconv.FormatInt(time.Now().UTC().UnixNano(), 36)
}
