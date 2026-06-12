package vm

import "context"

type BaseImageStore interface {
	SaveBaseImage(ctx context.Context, opts StartOptions) error
	RestoreFromBaseImage(ctx context.Context, opts StartOptions) error
	DeleteBaseImage(ctx context.Context) error
	HasBaseImage(ctx context.Context) bool
}

func AsBaseImageStore(v VM) (BaseImageStore, bool) {
	store, ok := v.(BaseImageStore)
	return store, ok
}
