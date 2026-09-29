package usecase_test

import (
	"context"

	"github.com/rikukaInoue/greenfield/services/photo/usecase"
)

// sinkEventual はイベントを受けるだけの Eventual。tx の検査（Publish が Do の中か）は
// core/consistency 側のテストが担うため、ここでは受理だけする。
type sinkEventual struct{}

func (sinkEventual) Publish(context.Context, usecase.Event) error { return nil }
