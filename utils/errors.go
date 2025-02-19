package utils

import (
	"strings"

	"github.com/go-playground/validator/v10"
)

func HandleValidationError(err error) string {
	if validationErrors, ok := err.(validator.ValidationErrors); ok {
		var errorMessages []string
		for _, e := range validationErrors {
			switch e.Tag() {
			case "required":
				errorMessages = append(errorMessages,
					"Field '"+e.Field()+"' is required")
			case "min":
				errorMessages = append(errorMessages,
					"Field '"+e.Field()+"' must be at least "+e.Param()+" characters long")
			case "max":
				errorMessages = append(errorMessages,
					"Field '"+e.Field()+"' must not exceed "+e.Param()+" characters")
			default:
				errorMessages = append(errorMessages,
					"Field '"+e.Field()+"' failed validation")
			}
		}
		return strings.Join(errorMessages, ". ")
	}
	return err.Error()
}
