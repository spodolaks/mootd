package wardrobe

import (
	"context"
	"fmt"
	"log"
	"time"
)

const (
	pngRetryInterval = 30 * time.Second
	// pngMaxAttempts caps per-item retries. After this many
	// failures the item is skipped permanently. 5 attempts × 30s
	// minimum between them = ~2.5min of wall-clock retries before
	// giving up. Realistic transient failures (rembg cold start,
	// brief network hiccup) clear well within this budget; stuck
	// items don't keep churning forever.
	pngMaxAttempts = 5
	// pngRetryAgeCap is the upper bound on how long after item
	// creation we keep retrying. After 7 days the user has likely
	// moved on; further retries are dead weight.
	pngRetryAgeCap = 7 * 24 * time.Hour
)

// StartPNGRetryWorker launches a background goroutine that retries bg removal
// for items that have an imageUrl but no pngImageUrl. It runs immediately on
// start, then repeats every 30s as long as there are items still missing PNGs.
// Stops when ctx is cancelled.
func StartPNGRetryWorker(ctx context.Context, repo Repository, bgRemover *BackgroundRemover, logger *log.Logger) {
	if bgRemover == nil {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Printf("wardrobe: png retry worker panic: %v", r)
			}
		}()
		// Run immediately — don't wait for first tick.
		hasMissing := retryMissingPNGs(ctx, repo, bgRemover, logger)
		if !hasMissing {
			return // nothing to do; new failures are picked up by the detect handler
		}

		ticker := time.NewTicker(pngRetryInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				hasMissing = retryMissingPNGs(ctx, repo, bgRemover, logger)
				if !hasMissing {
					return // all done
				}
			}
		}
	}()
}

// TriggerPNGRetry can be called after a failed bg removal to kick off a retry
// attempt without waiting for the next tick. It is non-blocking.
func TriggerPNGRetry(ctx context.Context, repo Repository, bgRemover *BackgroundRemover, logger *log.Logger) {
	if bgRemover == nil {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Printf("wardrobe: png retry trigger panic: %v", r)
			}
		}()
		retryMissingPNGs(ctx, repo, bgRemover, logger)
	}()
}

// retryMissingPNGs processes all items with empty pngImageUrl that
// are still within the retry budget. Returns true if there were
// eligible items to process (some may still have failed). Failures
// increment the per-item attempt counter so a poisoned image ages
// out instead of churning forever.
func retryMissingPNGs(ctx context.Context, repo Repository, bgRemover *BackgroundRemover, logger *log.Logger) bool {
	items, err := repo.FindMissingPNG(ctx, pngMaxAttempts, pngRetryAgeCap)
	if err != nil {
		logger.Printf("wardrobe: png retry: list items: %v", err)
		return false
	}
	if len(items) == 0 {
		return false
	}
	logger.Printf("wardrobe: png retry: found %d item(s) eligible for retry", len(items))

	for _, item := range items {
		if ctx.Err() != nil {
			return true
		}

		imgData, _, dlErr := repo.GetImage(ctx, item.ID)
		if dlErr != nil {
			reason := fmt.Sprintf("load image: %v", dlErr)
			logger.Printf("wardrobe: png retry: %s for item %s (attempts=%d)", reason, item.ID, item.PngAttempts+1)
			recordPngFailure(ctx, repo, item.ID, reason, logger)
			continue
		}

		pngData, bgErr := bgRemover.RemoveBackground(imgData, item.ID+".jpg")
		if bgErr != nil {
			reason := fmt.Sprintf("bg removal: %v", bgErr)
			logger.Printf("wardrobe: png retry: %s for item %s (attempts=%d)", reason, item.ID, item.PngAttempts+1)
			recordPngFailure(ctx, repo, item.ID, reason, logger)
			continue
		}

		if saveErr := repo.SaveImage(ctx, item.ID+"-png", pngData, "image/png"); saveErr != nil {
			reason := fmt.Sprintf("store png: %v", saveErr)
			logger.Printf("wardrobe: png retry: %s for item %s (attempts=%d)", reason, item.ID, item.PngAttempts+1)
			recordPngFailure(ctx, repo, item.ID, reason, logger)
			continue
		}

		pngURL := "/v1/wardrobe/items/" + item.ID + "-png/image"
		if updateErr := repo.UpdatePngURL(ctx, item.ID, pngURL); updateErr != nil {
			reason := fmt.Sprintf("update pngImageUrl: %v", updateErr)
			logger.Printf("wardrobe: png retry: %s for item %s (attempts=%d)", reason, item.ID, item.PngAttempts+1)
			recordPngFailure(ctx, repo, item.ID, reason, logger)
			continue
		}
		logger.Printf("wardrobe: png retry: success for item %s", item.ID)
	}
	// Return true so the caller keeps the ticker alive until a full clean pass.
	return true
}

// recordPngFailure is the small wrapper that propagates a failure
// reason to the repo. Splits out so the four error paths above stay
// readable. Stomps no log on failure of the failure record itself —
// just notes it; the worker has bigger problems if Mongo writes
// stop working entirely.
func recordPngFailure(ctx context.Context, repo Repository, id, reason string, logger *log.Logger) {
	if err := repo.RecordPngFailure(ctx, id, reason); err != nil {
		logger.Printf("wardrobe: png retry: record failure for item %s: %v", id, err)
	}
}
