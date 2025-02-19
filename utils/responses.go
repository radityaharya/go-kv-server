package utils

import (
	"kv-server/models"

	"github.com/gin-gonic/gin"
)


func SendError(c *gin.Context, code int, message string) {
	c.JSON(code, models.APIResponse{
		Status:  "error",
		Message: message,
	})
}

func SendSuccess(c *gin.Context, data interface{}) {
	response := models.APIResponse{
		Status: "success",
		Data:   data,
	}
	c.JSON(200, response)
}