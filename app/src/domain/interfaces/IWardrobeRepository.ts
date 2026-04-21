import type { ClothingDetectionResult, ClothingSearchProduct, Outfit, WardrobeItem } from '../models';

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
  getItems(params?: { limit?: number; cursor?: string }): Promise<{ items: WardrobeItem[]; nextCursor: string | null }>;

  /**
   * Update a wardrobe item's traits and optionally its label and image URL.
   * label and imageUrl are only sent when the user selected a search product.
   */
  updateItem(id: string, traits: Record<string, string>, label?: string, imageUrl?: string): Promise<void>;

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
   */
  submitOutfitGeneration(weather?: { temperature: number; condition: string; unit: string }): Promise<string>;

  /**
   * Poll an outfit generation job. Returns status + outfits when complete.
   */
  pollOutfitJob(jobId: string): Promise<{ status: 'pending' | 'processing' | 'completed' | 'failed'; outfits?: Outfit[]; error?: string }>;

  /**
   * Search the external catalog for products matching the given wardrobe item and brand.
   * @param itemId - wardrobe item ID (used to look up the stored image on the backend)
   * @param brand  - brand name entered by the user
   */
  searchByBrand(itemId: string, brand: string): Promise<ClothingSearchProduct[]>;
}
