import type {
  ClothingDetectionResult,
  ClothingSearchProduct,
  Outfit,
  WardrobeItem,
} from '../models';

/**
 * One snapshot of an in-flight outfit generation (mootd#62).
 * Mirrors the backend's GenerateProgress shape; cumulative,
 * not delta — a late-joining client sees consistent state from
 * any single event.
 */
export interface OutfitProgress {
  stage: 'connecting' | 'streaming' | 'done' | 'error';
  outfits?: Outfit[];
  description?: string;
}

/**
 * Abstraction over the wardrobe detection backend.
 * Swap between API and mock implementations via EXPO_PUBLIC_DATA_SOURCE.
 */
export interface IWardrobeRepository {
  /**
   * Upload an image and receive detected clothing items (synchronous).
   * The request blocks until detection finishes — fine for native callers
   * hitting the origin directly, but on web the ~100s Cloudflare edge
   * timeout will kill long-running detections. Prefer the async pair
   * below when running behind a CDN proxy.
   * @param imageUri - local file URI or data URI of the photo
   * @returns Detection result containing the list of identified items
   */
  detectClothing(imageUri: string): Promise<ClothingDetectionResult>;

  /**
   * Submit an image for asynchronous detection. Returns quickly (<1s)
   * with a job ID; poll pollDetectionJob until status is completed or
   * failed. Purpose-built for the CDN-proxied web path.
   * @param imageUri - local file URI or data URI of the photo
   * @returns Job ID to pass into pollDetectionJob
   */
  submitDetection(imageUri: string): Promise<string>;

  /**
   * Poll an async detection job. Returns status + items when complete.
   */
  pollDetectionJob(jobId: string): Promise<{
    status: 'pending' | 'processing' | 'completed' | 'failed';
    items?: ClothingDetectionResult['items'];
    error?: string;
  }>;

  /**
   * Fetch clothing items stored in the user's wardrobe with cursor pagination.
   * @param params.limit - Maximum number of items to return per page
   * @param params.cursor - Cursor for the next page (from a previous response)
   * @returns Items for the current page and a cursor for the next page (null if no more)
   */
  getItems(params?: {
    limit?: number;
    cursor?: string;
  }): Promise<{ items: WardrobeItem[]; nextCursor: string | null }>;

  /**
   * Fetch the caller's complete wardrobe by walking every page of
   * cursor-paginated `getItems()` results. Use this on screens that
   * need a full lookup table — Moodboard's collage, Calendar's
   * saved-board snapshots, Style Analysis — where missing an item
   * resolves to "Add top" placeholders or skews aggregations.
   *
   * Per-page limit defaults to 100 (the backend's max). The walk is
   * unbounded but in practice stops in 1-2 page hops; oversized
   * wardrobes hit the natural cap of `getItems` rather than a fresh
   * one here.
   */
  getAllItems(): Promise<WardrobeItem[]>;

  /**
   * Update a wardrobe item's traits and optionally its label and image URL.
   * label and imageUrl are only sent when the user selected a search product.
   */
  updateItem(
    id: string,
    traits: Record<string, string>,
    label?: string,
    imageUrl?: string
  ): Promise<void>;

  /**
   * Permanently remove a wardrobe item by ID.
   */
  deleteItem(id: string): Promise<void>;

  /**
   * Generate outfit suggestions for the current user using the AI stylist.
   * Optional weather context makes suggestions weather-appropriate.
   */
  getOutfits(weather?: { temperature: number; condition: string; unit: string }): Promise<Outfit[]>;

  /**
   * Submit async outfit generation job. Returns a job ID.
   *
   * `idempotencyKey` (optional) lets the caller dedupe a
   * Generate-button double-tap or a network retry. Mint a UUID
   * once per Generate press and pass it on every retry — the
   * backend (mootd#42) returns the original jobID inside a 60s
   * window instead of paying for a second LLM call.
   */
  submitOutfitGeneration(
    weather?: { temperature: number; condition: string; unit: string },
    idempotencyKey?: string
  ): Promise<string>;

  /**
   * Poll an outfit generation job. Returns status + outfits when complete.
   */
  pollOutfitJob(jobId: string): Promise<{
    status: 'pending' | 'processing' | 'completed' | 'failed';
    outfits?: Outfit[];
    error?: string;
  }>;

  /**
   * Stream outfit generation via SSE (mootd#62). Calls
   * `onProgress` once per server event with the cumulative
   * progress shape; resolves with the final outfits when the
   * generation completes. Throws on transport / generation
   * failure. Implementations that don't support streaming
   * should fall back to submit + poll under the hood.
   *
   * `idempotencyKey` is currently ignored on the streaming path
   * — the connection IS the dedupe key (a duplicate Generate
   * tap reaches a fresh stream and pays again). When this
   * matters we'll layer the same Idempotency-Key semantics.
   */
  streamOutfitGeneration?(
    onProgress: (p: OutfitProgress) => void,
    weather?: { temperature: number; condition: string; unit: string },
    idempotencyKey?: string
  ): Promise<Outfit[]>;

  /**
   * Search the external catalog for products matching the given wardrobe item and brand.
   * @param itemId - wardrobe item ID (used to look up the stored image on the backend)
   * @param brand  - brand name entered by the user
   */
  searchByBrand(itemId: string, brand: string): Promise<ClothingSearchProduct[]>;

  /**
   * "I have this IRL." Promotes a virtual archetype-default item
   * (id starts with `ad_`) into the user's wardrobe, returning the
   * resulting WardrobeItem with a freshly-minted `wi_<hex>` id.
   * Idempotent — calling twice for the same default returns the
   * SAME wardrobe row, never a duplicate. Use this when the user
   * taps the "in wardrobe" choice on a filler tile in a moodboard.
   * @param defaultId - the ad_<hex> id from the OutfitItem snapshot
   */
  claimArchetypeDefault(defaultId: string): Promise<WardrobeItem>;

  /**
   * "Not in my wardrobe." Records a per-user rejection so the
   * default never reappears in this user's outfit pool. Idempotent
   * — re-rejecting is a no-op 200. Use this when the user taps the
   * "not in wardrobe" choice on a filler tile.
   * @param defaultId - the ad_<hex> id from the OutfitItem snapshot
   */
  rejectArchetypeDefault(defaultId: string): Promise<void>;
}
