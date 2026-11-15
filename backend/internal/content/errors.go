package content

import "errors"

// Sentinel errors for the content store, mapped to HTTP statuses by the API
// layer.
var (
	// ErrConflict means the file changed after it was read (If-Match).
	ErrConflict = errors.New("content changed after it was read")
	// ErrExists means a create would overwrite an existing file.
	ErrExists = errors.New("content already exists")
	// ErrInUse prevents removing a category used by an article.
	ErrInUse = errors.New("content is in use")
)

// FieldError marks field-level validation failures mapped to 400
// validation_error by the API layer.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

func fieldError(field, message string) error {
	return &FieldError{Field: field, Message: message}
}
