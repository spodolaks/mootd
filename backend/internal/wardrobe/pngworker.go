package wardrobe

import (
	"context"
	"log"
	"time"
)

const pngRetryInterval = 30 * time.Second

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

// retryMissingPNGs processes all items with empty pngImageUrl.
// Returns true if there were items to process (some may still have failed).
func retryMissingPNGs(ctx context.Context, repo Repository, bgRemover *BackgroundRemover, logger *log.Logger) bool {
	items, err := repo.FindMissingPNG(ctx)
	if err != nil {
		logger.Printf("wardrobe: png retry: list items: %v", err)
		return false
	}
	if len(items) == 0 {
		return false
	}
	logger.Printf("wardrobe: png retry: found %d item(s) missing PNG", len(items))

	for _, item := range items {
		if ctx.Err() != nil {
			return true
		}

		imgData, _, dlErr := repo.GetImage(ctx, item.ID)
		if dlErr != nil {
			logger.Printf("wardrobe: png retry: load image for item %s: %v", item.ID, dlErr)
			continue
		}

		pngData, bgErr := bgRemover.RemoveBackground(imgData, item.ID+".jpg")
		if bgErr != nil {
			logger.Printf("wardrobe: png retry: bg removal for item %s: %v", item.ID, bgErr)
			continue
		}

		if saveErr := repo.SaveImage(ctx, item.ID+"-png", pngData, "image/png"); saveErr != nil {
			logger.Printf("wardrobe: png retry: store png for item %s: %v", item.ID, saveErr)
			continue
		}

		pngURL := "/v1/wardrobe/items/" + item.ID + "-png/image"
		if updateErr := repo.UpdatePngURL(ctx, item.ID, pngURL); updateErr != nil {
			logger.Printf("wardrobe: png retry: update pngImageUrl for item %s: %v", item.ID, updateErr)
			continue
		}
		logger.Printf("wardrobe: png retry: success for item %s", item.ID)
	}
	// Return true so the caller keeps the ticker alive until a full clean pass.
	return true
}
