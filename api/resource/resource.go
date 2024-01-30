package resource

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/disintegration/imaging"
	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/usememos/memos/internal/log"
	"github.com/usememos/memos/internal/util"
	"github.com/usememos/memos/server/profile"
	"github.com/usememos/memos/store"
)

const (
	// The key name used to store user id in the context
	// user id is extracted from the jwt token subject field.
	userIDContextKey = "user-id"
	// thumbnailImagePath is the directory to store image thumbnails.
	thumbnailImagePath = ".thumbnail_cache"
)

type ResourceService struct {
	Profile *profile.Profile
	Store   *store.Store
}

func NewResourceService(profile *profile.Profile, store *store.Store) *ResourceService {
	return &ResourceService{
		Profile: profile,
		Store:   store,
	}
}

func (s *ResourceService) RegisterRoutes(g *echo.Group) {
	g.GET("/r/:resourceName", s.streamResource)
	g.GET("/r/:resourceName/*", s.streamResource)
	g.GET("/s/:resourceID", s.streamResourceByID)
}

func (s *ResourceService) streamResourceByID(c echo.Context) error {
	ctx := c.Request().Context()
	resourceRawID := c.Param("resourceID")
	resourceID, err := strconv.Atoi(resourceRawID)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Errorf("resource ID %q is not integer", resourceRawID)).SetInternal(err)
	}
	var id = int32(resourceID)
	resource, err := s.Store.GetResource(ctx, &store.FindResource{
		ID:      &id,
		GetBlob: true,
	})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to find resource by id: %d", resourceID)).SetInternal(err)
	}
	if resource == nil {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("Resource not found: %d", resourceID))
	}
	return s.streamResourceContent(c, resource)
}

func (s *ResourceService) streamResource(c echo.Context) error {
	ctx := c.Request().Context()
	resourceName := c.Param("resourceName")
	resource, err := s.Store.GetResource(ctx, &store.FindResource{
		ResourceName: &resourceName,
		GetBlob:      true,
	})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to find resource by id: %s", resourceName)).SetInternal(err)
	}
	if resource == nil {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("Resource not found: %s", resourceName))
	}
	return s.streamResourceContent(c, resource)
}

func (s *ResourceService) streamResourceContent(c echo.Context, resource *store.Resource) error {
	ctx := c.Request().Context()
	// Check the related memo visibility.
	if resource.MemoID != nil {
		memo, err := s.Store.GetMemo(ctx, &store.FindMemo{
			ID: resource.MemoID,
		})
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to find memo by ID: %v", resource.MemoID)).SetInternal(err)
		}
		if memo != nil && memo.Visibility != store.Public {
			userID, ok := c.Get(userIDContextKey).(int32)
			if !ok || (memo.Visibility == store.Private && userID != resource.CreatorID) {
				return echo.NewHTTPError(http.StatusUnauthorized, "Resource visibility not match")
			}
		}
	}

	resourceStream, err := s.Store.GetResourceContent(ctx, resource)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get resource").SetInternal(err)
	}
	defer resourceStream.Close()

	if c.QueryParam("thumbnail") == "1" && util.HasPrefixes(resource.Type, "image/png", "image/jpeg") {
		ext := filepath.Ext(resource.Filename)
		thumbnailPath := filepath.Join(s.Profile.Data, thumbnailImagePath, fmt.Sprintf("%d%s", resource.ID, ext))
		thumbnailImage, err := getOrGenerateThumbnailImage(resourceStream, thumbnailPath)
		_ = resourceStream.Close() // we have to close stream anyway regardless of outcome
		if err != nil {
			log.Warn(fmt.Sprintf("failed to get or generate local thumbnail with path %s", thumbnailPath), zap.Error(err))
			// re-open stream and stream original content
			resourceStream, err = s.Store.GetResourceContent(ctx, resource)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get resource").SetInternal(err)
			}
			defer resourceStream.Close()
		} else {
			defer thumbnailImage.Close()
			resourceStream = thumbnailImage
		}
	}

	c.Response().Writer.Header().Set(echo.HeaderCacheControl, "max-age=3600")
	c.Response().Writer.Header().Set(echo.HeaderContentSecurityPolicy, "default-src 'none'; script-src 'none'; img-src 'self'; media-src 'self'; sandbox;")
	c.Response().Writer.Header().Set("Content-Disposition", fmt.Sprintf(`filename="%s"`, resource.Filename))
	resourceType := strings.ToLower(resource.Type)
	if strings.HasPrefix(resourceType, "text") {
		resourceType = echo.MIMETextPlainCharsetUTF8
	}
	if seeker, supportsSeek := resourceStream.(io.ReadSeeker); supportsSeek {
		// always serve content regardless of content type
		http.ServeContent(c.Response(), c.Request(), resource.Filename, time.Unix(resource.UpdatedTs, 0), seeker)
		return nil
	}
	return c.Stream(http.StatusOK, resourceType, resourceStream)
}

var availableGeneratorAmount int32 = 32

func getOrGenerateThumbnailImage(source io.Reader, dstPath string) (io.ReadCloser, error) {
	if _, err := os.Stat(dstPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, errors.Wrap(err, "failed to check thumbnail image stat")
		}

		if atomic.LoadInt32(&availableGeneratorAmount) <= 0 {
			return nil, errors.New("not enough available generator amount")
		}
		atomic.AddInt32(&availableGeneratorAmount, -1)
		defer func() {
			atomic.AddInt32(&availableGeneratorAmount, 1)
		}()

		src, err := imaging.Decode(source, imaging.AutoOrientation(true))
		if err != nil {
			return nil, errors.Wrap(err, "failed to decode thumbnail image")
		}
		thumbnailImage := imaging.Resize(src, 512, 0, imaging.Lanczos)

		dstDir := filepath.Dir(dstPath)
		if err := os.MkdirAll(dstDir, os.ModePerm); err != nil {
			return nil, errors.Wrap(err, "failed to create thumbnail dir")
		}

		if err := imaging.Save(thumbnailImage, dstPath); err != nil {
			return nil, errors.Wrap(err, "failed to resize thumbnail image")
		}
	}

	return os.Open(dstPath)
}
