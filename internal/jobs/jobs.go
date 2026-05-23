package jobs

import "context"

type Runner interface {
	EnqueueAnalysis(ctx context.Context, analysisID string) error
}
