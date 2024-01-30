// Package local provides functions for managing files locally on the file system.
package local

import (
	"context"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/pkg/errors"

	"github.com/usememos/memos/internal/resources/types"
)

const Name = "local" // type name for registry

var (
	_ types.ResourceProvider = &Local{} // compile time check that it implements interface
)

type Config struct {
	RootDir string `json:"root_dir"` // root dir for blobs
	Pattern string `json:"pattern"`  // optional pattern for files
	RawKey  bool   `json:"raw_key"`  // optional, DANGER (for backward compatibility), do not interpret keys for [Download] only
}

// New creates a new instance of the local resource provider with the given root directory.
func New(config *Config) *Local {
	return &Local{rootDir: config.RootDir, pattern: config.Pattern, rawKey: config.RawKey}
}

// Local struct represents a local file storage implementation.
type Local struct {
	rootDir string
	pattern string
	rawKey  bool
}

func (local *Local) Upload(_ context.Context, key string, payload io.Reader) error {
	resourcePath, err := local.getPath(key)
	if err != nil {
		return errors.Wrapf(err, "get path for %q", key)
	}
	dataDir := filepath.Dir(resourcePath)

	// create dir (if nested)
	if err := os.MkdirAll(dataDir, 0600); err != nil {
		return errors.Wrapf(err, "create base resource dir %q", dataDir)
	}

	// atomic write (all or nothing - no corrupted files, but may leave trash in case of crash)
	tempFile, err := os.CreateTemp(dataDir, filepath.Base(resourcePath)+".tmp.*")
	if err != nil {
		return errors.Wrapf(err, "create temp file")
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	if _, err := io.Copy(tempFile, payload); err != nil {
		return errors.Wrapf(err, "save content to temp file")
	}

	if err := tempFile.Close(); err != nil {
		return errors.Wrapf(err, "close and flush temp file")
	}

	if err := os.Rename(tempFile.Name(), resourcePath); err != nil {
		return errors.Wrapf(err, "move temp file to destination")
	}
	return nil
}

func (local *Local) Download(_ context.Context, key string) (io.ReadCloser, error) {
	var resourcePath string
	var err error
	if local.rawKey {
		// DANGER, used only for backward compatibility
		key = filepath.FromSlash(key)
		if !filepath.IsAbs(key) {
			key = filepath.Join(local.rootDir, key)
		}
	} else {
		resourcePath, err = local.getPath(key)
	}

	if err != nil {
		return nil, errors.Wrapf(err, "get path for %q", key)
	}

	src, err := os.Open(resourcePath)
	if os.IsNotExist(err) {
		err = types.ErrNotFound
	}
	if err != nil {
		return nil, errors.Wrapf(err, "open file %q", resourcePath)
	}
	return src, nil
}

func (local *Local) Delete(_ context.Context, key string) error {
	resourcePath, err := local.getPath(key)
	if err != nil {
		return errors.Wrapf(err, "get path for %q", key)
	}

	err = os.Remove(resourcePath)
	if os.IsNotExist(err) {
		err = nil
	}
	if err != nil {
		return errors.Wrapf(err, "remove file %q", resourcePath)
	}
	return nil
}

func (local *Local) getPath(key string) (string, error) {
	res := path.Clean(filepath.FromSlash(key)) // block bad actor access files outside local dir
	dir := filepath.Dir(res)
	base := filepath.Base(res)
	return filepath.Abs(filepath.Join(local.rootDir, dir, local.formatFile(base)))
}
