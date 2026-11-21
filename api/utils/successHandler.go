package utils

import "github.com/gin-gonic/gin"

// SuccessWithData answers {"status": "success", "data": data}.
func SuccessWithData(c *gin.Context, status int, data any) {
	c.JSON(status, gin.H{"status": "success", "data": data})
}
