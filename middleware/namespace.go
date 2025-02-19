package middleware

import (
	"kv-server/db"
	"kv-server/utils"
	"net/http"

	"github.com/gin-gonic/gin"
)

func NamespaceExists(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		ns := c.Param("namespace")
		exists, err := database.NamespaceExists(ns)
		if err != nil {
			utils.SendError(c, http.StatusInternalServerError, "Failed to check namespace: "+err.Error())
			c.Abort()
			return
		}
		if !exists {
			utils.SendError(c, http.StatusNotFound, "Namespace does not exist")
			c.Abort()
			return
		}
		c.Next()
	}
}
