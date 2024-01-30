// Package resources defines types and functions related to managing resources (blobs).
// Specific implementations can be found in underlying packages.
// The shared definitions are stored in [types] package.
package resources

import (
	"context"
	"encoding/json"
	"io"
	"sync"

	"github.com/pkg/errors"

	"github.com/usememos/memos/internal/resources/types"
)

// ErrProviderUnknown is a constant error which is used when was an attempt to create an unknown provider.
var ErrProviderUnknown = errors.New("unknown provider")

var providers sync.Map // name -> providerFactory

type providerFactory func([]byte) (types.ResourceProvider, error)

// RegisterProvider initializes the providers map (thread-safe) with the given name and factory function.
func RegisterProvider[T any, Res types.ResourceProvider](name string, factory func(config *T) Res) {
	providers.Store(name, providerFactory(func(bytes []byte) (types.ResourceProvider, error) {
		var config T
		if err := json.Unmarshal(bytes, &config); err != nil {
			return nil, errors.Wrapf(err, "decode config for provider %q", name)
		}
		return factory(&config), nil
	}))
}

// CreateProvider creates a ResourceProvider by name from the providers map, or return an error if it's not found.
// The config parameter should be JSON object and will be used to unmarshall to the specific configuration type.
func CreateProvider(name string, config []byte) (types.ResourceProvider, error) {
	factory, exists := providers.Load(name)
	if !exists {
		return nil, errors.Wrapf(ErrProviderUnknown, "get provider %q", name)
	}
	fn, ok := factory.(providerFactory)
	if !ok {
		// actually we should panic here, but let's keep it running
		return nil, errors.Wrapf(ErrProviderUnknown, "case provider %q to factory", name)
	}

	return fn(config)
}

// Content wraps [CreateProvider] and [types.ResourceProvider.Download] in one action for convenience.
func Content(ctx context.Context, provider string, config string, key string) (io.ReadCloser, error) {
	p, err := CreateProvider(provider, []byte(config))
	if err != nil {
		return nil, errors.Wrapf(err, "create provider %q", provider)
	}
	return p.Download(ctx, key)
}
