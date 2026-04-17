/**
 * A single clothing item detected from an uploaded photo.
 */
export interface DetectedClothingItem {
  /** Unique identifier returned by the detection API */
  id: string;
  /** Clothing category, e.g. "blazer", "shirt", "pants" */
  category: string;
  /** Human-readable label, e.g. "Slim Fit Blazer" */
  label: string;
  /** Detection confidence score (0 – 1). Optional – not every provider returns it. */
  confidence?: number;
  /** URL or local URI to a cropped JPEG image of the item */
  imageUrl?: string;
  /** URL to the background-removed PNG */
  pngImageUrl?: string;
  /** Traits detected by the service (color, material, fit, brand, etc.) */
  traits?: Record<string, string>;
}

/**
 * The full response from the clothing detection API.
 */
export interface ClothingDetectionResult {
  /** Detected clothing items */
  items: DetectedClothingItem[];
  /** The URI of the original photo the user uploaded */
  originalImageUri: string;
}
