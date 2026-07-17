package llm

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// FallbackClient tries a primary LLM client and, if it fails for any reason
// (quota exhaustion, rate limits, transient outages), retries the same
// request against a fallback client instead of failing the whole operation.
// This applies to every Chat() call made through the generator — topic ideas,
// draft writing, semantic duplicate checks, and image-scene descriptions all
// get the same graceful degradation.
type FallbackClient struct {
	primary  Client
	fallback Client
	logger   *zap.Logger
}

func NewFallbackClient(primary, fallback Client, logger *zap.Logger) *FallbackClient {
	return &FallbackClient{primary: primary, fallback: fallback, logger: logger}
}

func (f *FallbackClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	resp, err := f.primary.Chat(ctx, req)
	if err == nil {
		return resp, nil
	}

	f.logger.Warn("Primary LLM failed, retrying with fallback",
		zap.String("primary", f.primary.Provider()),
		zap.String("fallback", f.fallback.Provider()),
		zap.Error(err))

	resp, ferr := f.fallback.Chat(ctx, req)
	if ferr != nil {
		return nil, fmt.Errorf("primary (%s) failed: %v; fallback (%s) also failed: %w",
			f.primary.Provider(), err, f.fallback.Provider(), ferr)
	}
	return resp, nil
}

func (f *FallbackClient) Provider() string {
	return f.primary.Provider() + " (fallback: " + f.fallback.Provider() + ")"
}
