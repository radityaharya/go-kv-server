package main

import (
	"database/sql"
	"log"
	"net/http"
	"strconv"

	"kv-server/db"
	"kv-server/middleware"
	"kv-server/models"
	"kv-server/utils"

	"github.com/gin-gonic/gin"
)

type Handlers struct {
	db *db.DB
}

// Namespace handlers
func (h *Handlers) createNamespace(c *gin.Context) {
	name := c.Param("name")
	if err := h.db.CreateNamespace(name); err != nil {
		utils.SendError(c, http.StatusConflict, "Namespace already exists")
		return
	}
	utils.SendSuccess(c, nil)
}

func (h *Handlers) deleteNamespace(c *gin.Context) {
	name := c.Param("name")
	if err := h.db.DeleteNamespace(name); err != nil {
		utils.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}
	utils.SendSuccess(c, nil)
}

func (h *Handlers) listNamespaces(c *gin.Context) {
	names, err := h.db.ListNamespaces()
	if err != nil {
		utils.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}
	utils.SendSuccess(c, names)
}

// Key-value handlers
func (h *Handlers) getValue(c *gin.Context) {
	ns := c.Param("namespace")
	key := c.Param("key")

	value, err := h.db.GetValue(ns, key)
	if err == sql.ErrNoRows {
		utils.SendError(c, http.StatusNotFound, "Key does not exist in namespace")
		return
	}
	if err != nil {
		utils.SendError(c, http.StatusInternalServerError, "Failed to get value: "+err.Error())
		return
	}
	utils.SendSuccess(c, gin.H{"value": value})
}

func (h *Handlers) getAllValues(c *gin.Context) {
	ns := c.Param("namespace")
	limitStr := c.DefaultQuery("limit", "10")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		utils.SendError(c, http.StatusBadRequest, "Invalid limit")
		return
	}
	offset, err := strconv.Atoi(offsetStr)
	if err != nil {
		utils.SendError(c, http.StatusBadRequest, "Invalid offset")
		return
	}

	values, err := h.db.GetAllValuesPaginated(ns, limit, offset)
	if err != nil {
		utils.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}
	utils.SendSuccess(c, values)
}

func (h *Handlers) setValue(c *gin.Context) {
	ns := c.Param("namespace")
	var request models.KeyValueRequest

	if err := c.ShouldBindJSON(&request); err != nil {
		utils.SendError(c, http.StatusBadRequest, utils.HandleValidationError(err))
		return
	}

	if err := h.db.SetValue(ns, request.Key, request.Value); err != nil {
		utils.SendError(c, http.StatusInternalServerError, "Failed to set value: "+err.Error())
		return
	}
	utils.SendSuccess(c, nil)
}

func (h *Handlers) deleteValue(c *gin.Context) {
	ns := c.Param("namespace")
	key := c.Param("key")

	if err := h.db.DeleteValue(ns, key); err != nil {
		utils.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}
	utils.SendSuccess(c, nil)
}

// Stats handlers
func (h *Handlers) getNamespaceCount(c *gin.Context) {
	ns := c.Param("namespace")
	count, err := h.db.CountValuesInNamespace(ns)
	if err != nil {
		utils.SendError(c, http.StatusInternalServerError, "Failed to count values: "+err.Error())
		return
	}
	utils.SendSuccess(c, count)
}

func (h *Handlers) getStats(c *gin.Context) {
	nsCount, err := h.db.CountNamespaces()
	if err != nil {
		utils.SendError(c, http.StatusInternalServerError, "Failed to count namespaces: "+err.Error())
		return
	}
	kvCount, err := h.db.CountKeyValues()
	if err != nil {
		utils.SendError(c, http.StatusInternalServerError, "Failed to count key-values: "+err.Error())
		return
	}
	stats := map[string]int{
		"namespaces": nsCount,
		"keyValues":  kvCount,
	}
	utils.SendSuccess(c, stats)
}

func setupRoutes(router *gin.Engine, h *Handlers) {
	// Static routes
	router.GET("/", func(c *gin.Context) { c.File("./static/index.html") })
	router.GET("/favicon.ico", func(c *gin.Context) { c.File("./static/favicon.ico") })

	// API routes
	api := router.Group("/api")
	{
		// Namespace routes
		api.POST("/namespace/:name", h.createNamespace)
		api.DELETE("/namespace/:name", h.deleteNamespace)
		api.GET("/namespaces", h.listNamespaces)

		// namespace routes with middleware
		nsRoutes := api.Group("/ns/:namespace")
		nsRoutes.Use(middleware.NamespaceExists(h.db))
		{
			nsRoutes.GET("/get/:key", h.getValue)
			nsRoutes.GET("/get-all", h.getAllValues)
			nsRoutes.POST("/set", h.setValue)
			nsRoutes.DELETE("/delete/:key", h.deleteValue)
			nsRoutes.GET("/count", h.getNamespaceCount)
		}

		api.GET("/stats", h.getStats)
	}
}

func main() {
	database, err := db.InitDB("data/kv-store.db")
	if err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer database.Close()

	if err := database.CreateTables(); err != nil {
		log.Fatal("Failed to create tables:", err)
	}

	handlers := &Handlers{db: database}
	router := gin.Default()

	setupRoutes(router, handlers)

	log.Println("Server starting on :8080")
	if err := router.Run(":8080"); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
