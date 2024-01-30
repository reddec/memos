package types

import (
	"context"
	"errors"
	"io"
)

var ErrNotFound = errors.New("resource not found")

// ResourceProvider type definition provides an interface for managing resources with keys,
// allowing operations such as uploading, downloading and deleting.
// Upload method stores provided payload with the given key.
// Download method retrieves content addressed by the specified key, returning a readable stream.
// Error [ErrNotFound] (possibly wrapped) is expected in case of non-existent content.
// Delete method removes content associated with the provided key.
// Deleting non-existing keys should not result in errors
type ResourceProvider interface {
	// Upload stores provided payload addressed by key.
	// Implementation may impose restrictions on key.
	// Implementation should not make assumptions about payload size and expect that it will be much bigger
	// than available RAM.
	Upload(ctx context.Context, key string, payload io.Reader) error
	// Download content addressed by key.
	// The returned error in case of content absence should be [ErrNotFound] (possibly wrapped).
	// If error returned, the stream must be nil.
	// It's caller responsibility to close stream in case of successful invocation.
	// If returned type supports [io.ReadSeeker] interface, it will be used to optimize streaming.
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete content addressed by key. Deleting non-existent key should NOT cause an error.
	Delete(ctx context.Context, key string) error
}

func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}
