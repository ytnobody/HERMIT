package github

import (
	"context"
	"fmt"
	"log"
	"time"
)

const (
	defaultRateLimitThreshold = 10
	maxWaitDuration           = 10 * time.Minute
)

// sleepFunc is used for waiting; replaced in tests to avoid real sleeps.
var sleepFunc = time.Sleep

// RateLimitInfo holds the current rate limit status from GitHub API.
type RateLimitInfo struct {
	Remaining int
	ResetAt   time.Time
}

// CheckRateLimit checks the GitHub API rate limit and waits or returns an error
// if the remaining count is at or below the threshold.
// If the check itself fails, it logs a warning and returns nil (fail-open).
func (c *Client) CheckRateLimit(threshold int) error {
	if threshold <= 0 {
		threshold = defaultRateLimitThreshold
	}

	info, err := c.getRateLimitInfo()
	if err != nil {
		log.Printf("warn: failed to check rate limit: %v", err)
		return nil
	}

	if info.Remaining > threshold {
		return nil
	}

	waitDuration := time.Until(info.ResetAt)
	if waitDuration <= maxWaitDuration {
		if waitDuration > 0 {
			log.Printf("info: rate limit low (%d remaining), waiting %v for reset", info.Remaining, waitDuration.Round(time.Second))
			sleepFunc(waitDuration)
		}
		return nil
	}

	return fmt.Errorf("rate limit too low (%d remaining) and reset is too far away (%v); skipping cycle", info.Remaining, waitDuration.Round(time.Second))
}

func (c *Client) getRateLimitInfo() (*RateLimitInfo, error) {
	limits, _, err := c.gh.RateLimit.Get(context.Background())
	if err != nil {
		return nil, err
	}
	core := limits.GetCore()
	if core == nil {
		return nil, fmt.Errorf("core rate limit info is nil")
	}
	return &RateLimitInfo{
		Remaining: core.Remaining,
		ResetAt:   core.Reset.Time,
	}, nil
}
