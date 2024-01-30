package store

import (
	"context"
	"encoding/json"

	"github.com/pkg/errors"

	"github.com/usememos/memos/internal/resources/local"
)

// MigrateLocalResourcesToStorages creates local storage and assigns all resource with InternalPath to it.
// For migration from 0.19
// TODO: remove after 1.0.0
func MigrateLocalResourcesToStorages(ctx context.Context, store *Store) error {
	// to avoid import cycle we have to copy config parameter
	const confLocalStorage = "local-storage-path"

	stores, err := store.ListStorages(ctx, &FindStorage{})
	if err != nil {
		return errors.Wrapf(err, "list storages")
	}
	var localStore *int32
	for _, s := range stores {
		if s.Type == local.Name {
			localStore = &s.ID
			break
		}
	}

	// unconditionally create local store - no impact anyway until first write
	if localStore == nil {

		systemSettingLocalStoragePath, err := store.GetWorkspaceSetting(ctx, &FindWorkspaceSetting{Name: confLocalStorage})
		if err != nil {
			return errors.Wrap(err, "Failed to find SystemSettingLocalStoragePathName")
		}
		localStoragePath := "assets/{timestamp}_{filename}"
		if systemSettingLocalStoragePath != nil && systemSettingLocalStoragePath.Value != "" {
			err = json.Unmarshal([]byte(systemSettingLocalStoragePath.Value), &localStoragePath)
			if err != nil {
				return errors.Wrap(err, "Failed to unmarshal SystemSettingLocalStoragePathName")
			}
		}

		defaultConfig, err := json.Marshal(local.Config{
			RootDir: store.Profile.Data,
			Pattern: localStoragePath,
			RawKey:  true,
		})
		if err != nil {
			return errors.Wrapf(err, "create default config for local storage")
		}

		// create new default store
		newStore, err := store.CreateStorage(ctx, &Storage{
			Name:   "Local",
			Type:   local.Name,
			Config: string(defaultConfig),
		})
		if err != nil {
			return errors.Wrapf(err, "create new local storage")
		}
		localStore = &newStore.ID
	}

	// scan resources
	err = iterateResources(ctx, store, FindResource{GetBlob: false}, func(res *Resource) error {
		if res.InternalPath != "" && res.StorageID == nil {
			res.StorageID = localStore
			_, err = store.UpdateResource(ctx, &UpdateResource{
				ID:        res.ID,
				StorageID: localStore,
			})
			return err
		}
		return nil
	})
	if err != nil {
		return errors.Wrapf(err, "migrate local resources")
	}
	return nil
}

func iterateResources(ctx context.Context, store *Store, query FindResource, handler func(res *Resource) error) error {
	const pageSize = 32
	var limit = pageSize
	query.Limit = &limit
	if query.Offset == nil {
		query.Offset = new(int)
	}
	for {
		resources, err := store.ListResources(ctx, &query)
		if err != nil {
			return errors.Wrapf(err, "list resources, offset %d", *query.Offset)
		}

		for _, res := range resources {
			if err := handler(res); err != nil {
				return errors.Wrapf(err, "process resource %d", res.ID)
			}
		}

		*query.Offset += limit
		if len(resources) < limit {
			break
		}
	}
	return nil
}
