package models

type KeyValueRequest struct {
	Key   string `json:"key" binding:"required,min=1,max=255"`
	Value string `json:"value" binding:"required"`
}
