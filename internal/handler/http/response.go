package http

import "github.com/gin-gonic/gin"

// Response provides standardized API response helpers.
// Ensures consistent JSON structure across all endpoints.

// Success sends a 200 OK response with data.
func Success(c *gin.Context, data interface{}) {
	c.JSON(200, data)
}

// Created sends a 201 Created response with data.
func Created(c *gin.Context, data interface{}) {
	c.JSON(201, data)
}

// Error sends an error response with the given status code and message.
// Automatically adds "error" field to JSON response.
func Error(c *gin.Context, code int, message string) {
	c.JSON(code, gin.H{"error": message})
}

// BadRequest sends a 400 Bad Request error.
func BadRequest(c *gin.Context, message string) {
	Error(c, 400, message)
}

// NotFound sends a 404 Not Found error.
func NotFound(c *gin.Context, message string) {
	Error(c, 404, message)
}

// InternalError sends a 500 Internal Server Error.
func InternalError(c *gin.Context, message string) {
	Error(c, 500, message)
}
