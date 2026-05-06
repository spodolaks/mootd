package wardrobe

import "context"

// DetectorBackend is the interface the wardrobe handler depends
// on for clothing detection. Two implementations satisfy it:
//
//   - *Detector — legacy local-API path (analyze + generate
//     against the on-host cloth-detection service).
//   - *SingleItemDetector — calls a separate orchestrator
//     service (singleItemDetection). Selected via the
//     DETECTION_BACKEND env var at boot.
//
// Keeping the surface narrow lets future implementations
// (queue-driven, on-device with native ML, etc.) drop in
// without changing the handler.
type DetectorBackend interface {
	// Detect submits an image and returns the detected items
	// plus a per-call archive blob the handler writes onto the
	// detection_run row. userID + runID are observability
	// stamps; runID is minted upfront so the run row, the
	// input-image upload, and the per-item llm_calls all join.
	Detect(ctx context.Context, userID, runID string, imageData []byte, filename string) ([]jobItem, *DetectionRunData, error)
}

// Compile-time check that the legacy *Detector satisfies the
// interface. New implementations should mirror this guard.
var _ DetectorBackend = (*Detector)(nil)
