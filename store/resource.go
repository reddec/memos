package store

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/pkg/errors"

	"github.com/usememos/memos/internal/resources"
	"github.com/usememos/memos/internal/util"
)

const (
	// thumbnailImagePath is the directory to store image thumbnails.
	thumbnailImagePath = ".thumbnail_cache"
)

type Resource struct {
	ID           int32
	ResourceName string
	StorageID    *int32

	// Standard fields
	CreatorID int32
	CreatedTs int64
	UpdatedTs int64

	// Domain specific fields
	Filename     string
	Blob         []byte
	InternalPath string
	ExternalLink string
	Type         string
	Size         int64
	MemoID       *int32
}

type FindResource struct {
	GetBlob        bool
	ID             *int32
	ResourceName   *string
	CreatorID      *int32
	Filename       *string
	MemoID         *int32
	HasRelatedMemo bool
	Limit          *int
	Offset         *int
}

type UpdateResource struct {
	ID           int32
	ResourceName *string
	UpdatedTs    *int64
	Filename     *string
	InternalPath *string
	ExternalLink *string
	StorageID    *int32
	MemoID       *int32
	Blob         []byte
}

type DeleteResource struct {
	ID     int32
	MemoID *int32
}

func (s *Store) CreateResource(ctx context.Context, create *Resource) (*Resource, error) {
	if !util.ResourceNameMatcher.MatchString(create.ResourceName) {
		return nil, errors.New("invalid resource name")
	}
	return s.driver.CreateResource(ctx, create)
}

func (s *Store) ListResources(ctx context.Context, find *FindResource) ([]*Resource, error) {
	return s.driver.ListResources(ctx, find)
}

func (s *Store) GetResource(ctx context.Context, find *FindResource) (*Resource, error) {
	resources, err := s.ListResources(ctx, find)
	if err != nil {
		return nil, err
	}

	if len(resources) == 0 {
		return nil, nil
	}

	return resources[0], nil
}

func (s *Store) UpdateResource(ctx context.Context, update *UpdateResource) (*Resource, error) {
	if update.ResourceName != nil && !util.ResourceNameMatcher.MatchString(*update.ResourceName) {
		return nil, errors.New("invalid resource name")
	}
	return s.driver.UpdateResource(ctx, update)
}

func (s *Store) DeleteResource(ctx context.Context, delete *DeleteResource) error {
	resource, err := s.GetResource(ctx, &FindResource{ID: &delete.ID})
	if err != nil {
		return errors.Wrap(err, "failed to get resource")
	}
	if resource == nil {
		return errors.Wrap(nil, "resource not found")
	}

	// Delete the local file.
	if resource.InternalPath != "" {
		resourcePath := filepath.FromSlash(resource.InternalPath)
		if !filepath.IsAbs(resourcePath) {
			resourcePath = filepath.Join(s.Profile.Data, resourcePath)
		}
		_ = os.Remove(resourcePath)
	}

	// Delete the thumbnail.
	if util.HasPrefixes(resource.Type, "image/png", "image/jpeg") {
		ext := filepath.Ext(resource.Filename)
		thumbnailPath := filepath.Join(s.Profile.Data, thumbnailImagePath, fmt.Sprintf("%d%s", resource.ID, ext))
		_ = os.Remove(thumbnailPath)
	}
	return s.driver.DeleteResource(ctx, delete)
}

func (s *Store) GetResourceContent(ctx context.Context, r *Resource) (io.ReadCloser, error) {
	// try new approach
	if r.StorageID != nil {
		storage, err := s.GetStorage(ctx, &FindStorage{ID: r.StorageID})
		if err != nil {
			return nil, errors.Wrapf(err, "find storage %d", *r.StorageID)
		}
		return resources.Content(ctx, storage.Type, storage.Config, r.ExternalLink)
	}
	if r.Blob != nil {
		// embedded to DB
		return io.NopCloser(bytes.NewReader(r.Blob)), nil
	}
	// legacy
	//
	if r.InternalPath != "" {
		// local file
		resourcePath := filepath.FromSlash(r.InternalPath)
		if !filepath.IsAbs(resourcePath) {
			resourcePath = filepath.Join(s.Profile.Data, resourcePath)
		}
		return os.Open(resourcePath)
	}
	if r.ExternalLink != "" {
		// external file
		return openLink(ctx, r.ExternalLink)
	}
	return nil, os.ErrNotExist
}

func openLink(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "create request to %q", url)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "execute request to %q", url)
	}
	if res.StatusCode/100 != 2 {
		_ = res.Body.Close()
		return nil, errors.Errorf("status code %d", res.StatusCode)
	}
	return res.Body, nil
}
