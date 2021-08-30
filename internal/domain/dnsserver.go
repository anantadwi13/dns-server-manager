package domain

import "context"

type DNSServer interface {
	UpdateConfigs(ctx context.Context) error
	Reload(ctx context.Context) error
	UpdateAndReload(ctx context.Context) error
	Shutdown(ctx context.Context) error
}
