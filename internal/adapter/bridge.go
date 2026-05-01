package adapter

import (
	"context"

	"dd-core/internal/model"
)

type ProtocolBridge interface {
	Start(ctx context.Context) error
	Stop() error
	Protocol() model.DdProtocol
}
