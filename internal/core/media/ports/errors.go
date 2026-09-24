// Package ports declares media storage, repository, and service interfaces.
package ports

import "errors"

// ErrObjectNotFound means the storage object does not exist.
var ErrObjectNotFound = errors.New("object not found")
