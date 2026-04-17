import type {
  ClothingSearchProduct,
  Outfit,
  ClothingDetectionResult,
  DetectedClothingItem,
  IWardrobeRepository,
  WardrobeItem,
} from '@/src/domain';
import { Platform } from 'react-native';
import { ApiError, apiClient, getApiBaseURL, getAuthToken } from '@/src/data/api/client';

interface DetectAPIResponse {
  items: Array<{
    id: string;
    category: string;
    label: string;
    confidence?: number;
    imageUrl?: string;
    pngImageUrl?: string;
    traits?: Record<string, string>;
  }>;
}

/**
 * Real API implementation that sends the image to the backend for detection.
 *
 * Expected endpoint: POST /v1/wardrobe/detect
 * Body: multipart/form-data with field "image"
 * Response: { items: DetectedClothingItem[] }
 */
const DETECT_TIMEOUT_MS = 4 * 60 * 1000; // detection can take up to 4 min on the backend
// Outfit generation now runs through Claude Sonnet (or local Ollama as a
// fallback). Sonnet typically returns in 2-10 s; the local model in 30-90 s.
// 60 s is a comfortable cap for both — anything beyond that should fail fast
// and surface the error to the user instead of hanging the screen.
const OUTFITS_TIMEOUT_MS = 60 * 1000;

// Convert a relative image path (/v1/wardrobe/items/{id}/image) to an absolute URL.
const toAbsoluteImageURL = (imageUrl: string | undefined): string => {
  if (!imageUrl) return '';
  if (imageUrl.startsWith('http://') || imageUrl.startsWith('https://')) return imageUrl;
  return `${getApiBaseURL()}${imageUrl}`;
};

export class ApiWardrobeRepository implements IWardrobeRepository {
  async detectClothing(imageUri: string): Promise<ClothingDetectionResult> {
    const startTime = Date.now();
    const elapsed = () => `${((Date.now() - startTime) / 1000).toFixed(1)}s`;

    console.log('[Wardrobe] Starting clothing detection — uploading photo...');

    const formData = new FormData();
    if (Platform.OS === 'web') {
      // On web, imageUri from ImagePicker is a data: or blob: URI.
      // We must fetch it to get a real Blob — plain objects are not supported by
      // the browser's FormData.
      console.log('[Wardrobe] Web platform — converting URI to Blob...');
      const res = await fetch(imageUri);
      const blob = await res.blob();
      formData.append('image', blob, 'photo.jpg');
    } else {
      // On native, React Native's FormData understands { uri, name, type } and
      // reads the file from the local filesystem path.
      formData.append('image', {
        uri: imageUri,
        name: 'photo.jpg',
        type: 'image/jpeg',
      } as unknown as Blob);
    }

    console.log('[Wardrobe] → POST /v1/wardrobe/detect (backend will poll detection service every 3 s)');

    // Log a heartbeat every 3 s while the backend polls the detection service.
    const heartbeat = setInterval(() => {
      console.log(`[Wardrobe] ⏳ Waiting for detection result... ${elapsed()} elapsed`);
    }, 3000);

    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), DETECT_TIMEOUT_MS);

    let rawResponse: Response;
    try {
      // Use raw fetch for multipart upload so the runtime sets the correct
      // Content-Type: multipart/form-data; boundary=... header automatically.
      // apiClient.postFormData passes a headers object which can interfere with
      // the boundary generation on some React Native / web versions.
      rawResponse = await fetch(`${getApiBaseURL()}/v1/wardrobe/detect`, {
        method: 'POST',
        body: formData,
        signal: controller.signal,
        headers: {
          // Only the auth header — no Content-Type override.
          ...(getAuthToken() ? { Authorization: `Bearer ${getAuthToken()}` } : {}),
        },
      });
    } finally {
      clearInterval(heartbeat);
      clearTimeout(timeoutId);
    }

    const body = await rawResponse.json().catch(() => ({})) as Record<string, unknown>;

    if (!rawResponse.ok) {
      const msg =
        typeof body.error === 'string' ? body.error : `Detection failed (${rawResponse.status})`;
      console.log(`[Wardrobe] ✗ ${rawResponse.status} — ${msg}`);
      throw new ApiError(msg, rawResponse.status, body);
    }

    const data = body as { items: DetectAPIResponse['items'] };
    console.log(
      `[Wardrobe] ✓ Response received in ${elapsed()} — ${data.items?.length ?? 0} item(s) detected`,
    );
    (data.items ?? []).forEach((item, i) => {
      console.log(
        `[Wardrobe]   [${i + 1}] ${item.label} (${item.category})` +
          (item.confidence !== undefined ? ` confidence=${(item.confidence * 100).toFixed(0)}%` : ''),
      );
    });

    const items: DetectedClothingItem[] = (data.items ?? []).map(item => ({
      id: item.id,
      category: item.category,
      label: item.label,
      confidence: item.confidence,
      imageUrl: toAbsoluteImageURL(item.imageUrl),
      pngImageUrl: toAbsoluteImageURL(item.pngImageUrl) || undefined,
      traits: item.traits,
    }));

    return { originalImageUri: imageUri, items };
  }

  async getItems(params?: { limit?: number; cursor?: string }): Promise<{ items: WardrobeItem[]; nextCursor: string | null }> {
    const qs = new URLSearchParams();
    if (params?.limit) qs.set('limit', String(params.limit));
    if (params?.cursor) qs.set('cursor', params.cursor);
    const query = qs.toString();
    const url = `/v1/wardrobe/items${query ? `?${query}` : ''}`;

    console.log(`[Wardrobe] → GET ${url}`);
    const response = await apiClient.get<{ items: WardrobeItem[]; nextCursor: string | null }>(url);
    const items = (response.items ?? []).map(item => ({
      ...item,
      imageUrl: toAbsoluteImageURL(item.imageUrl),
      pngImageUrl: toAbsoluteImageURL(item.pngImageUrl) || undefined,
    }));
    console.log(`[Wardrobe] ✓ Wardrobe loaded — ${items.length} item(s), nextCursor=${response.nextCursor ? 'yes' : 'none'}`);
    return { items, nextCursor: response.nextCursor ?? null };
  }

  async updateItem(id: string, traits: Record<string, string>, label?: string, imageUrl?: string): Promise<void> {
    console.log(`[Wardrobe] → PATCH /v1/wardrobe/items/${id}`);
    await apiClient.patch(`/v1/wardrobe/items/${id}`, {
      traits,
      ...(label ? { label } : {}),
      ...(imageUrl ? { imageUrl } : {}),
    });
    console.log(`[Wardrobe] ✓ Item ${id} updated`);
  }

  async deleteItem(id: string): Promise<void> {
    console.log(`[Wardrobe] → DELETE /v1/wardrobe/items/${id}`);
    await apiClient.delete(`/v1/wardrobe/items/${id}`);
    console.log(`[Wardrobe] ✓ Item ${id} deleted`);
  }

  async searchByBrand(itemId: string, brand: string): Promise<ClothingSearchProduct[]> {
    console.log(`[Wardrobe] → POST /v1/wardrobe/items/${itemId}/search brand=${brand}`);
    const result = await apiClient.post<{ results: ClothingSearchProduct[] }>(
      `/v1/wardrobe/items/${itemId}/search`,
      { brand },
    );
    const results = result.results ?? [];
    console.log(`[Wardrobe] ✓ Search returned ${results.length} product(s)`);
    return results;
  }

  async submitOutfitGeneration(weather?: { temperature: number; condition: string; unit: string }): Promise<string> {
    const params = new URLSearchParams();
    if (weather) {
      params.set('temperature', String(Math.round(weather.temperature)));
      params.set('condition', weather.condition);
      params.set('unit', weather.unit === 'fahrenheit' ? 'F' : 'C');
    }
    const qs = params.toString();
    const url = `/v1/outfits/generate${qs ? `?${qs}` : ''}`;
    console.log(`[Wardrobe] → POST ${url}`);
    const response = await apiClient.post<{ jobId: string }>(url, {});
    console.log(`[Wardrobe] ✓ Job submitted: ${response.jobId}`);
    return response.jobId;
  }

  async pollOutfitJob(jobId: string): Promise<{ status: 'pending' | 'processing' | 'completed' | 'failed'; outfits?: Outfit[]; error?: string }> {
    const raw = await apiClient.get<{ status: 'pending' | 'processing' | 'completed' | 'failed'; outfits?: Outfit[]; error?: string }>(`/v1/outfits/jobs/${jobId}`);
    if (raw.outfits) {
      raw.outfits = raw.outfits.map(hydrateOutfitUrls);
    }
    return raw;
  }

  async getOutfits(weather?: { temperature: number; condition: string; unit: string }): Promise<Outfit[]> {
    const params = new URLSearchParams();
    if (weather) {
      params.set('temperature', String(Math.round(weather.temperature)));
      params.set('condition', weather.condition);
      params.set('unit', weather.unit === 'fahrenheit' ? 'F' : 'C');
    }
    const qs = params.toString();
    const url = `${getApiBaseURL()}/v1/outfits${qs ? `?${qs}` : ''}`;

    console.log(`[Wardrobe] → GET ${url}`);
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), OUTFITS_TIMEOUT_MS);

    let rawResponse: Response;
    try {
      rawResponse = await fetch(url, {
        method: 'GET',
        signal: controller.signal,
        headers: {
          'Content-Type': 'application/json',
          ...(getAuthToken() ? { Authorization: `Bearer ${getAuthToken()}` } : {}),
        },
      });
    } finally {
      clearTimeout(timeoutId);
    }

    const body = await rawResponse.json().catch(() => ({})) as Record<string, unknown>;
    if (!rawResponse.ok) {
      const msg = typeof body.error === 'string' ? body.error : `Outfits failed (${rawResponse.status})`;
      throw new ApiError(msg, rawResponse.status, body);
    }

    const outfits = ((body.outfits as Outfit[] | undefined) ?? []).map(hydrateOutfitUrls);
    console.log(`[Wardrobe] ✓ Outfits received — ${outfits.length} outfit(s)`, outfits);
    return outfits;
  }
}

// hydrateOutfitUrls rewrites backend-relative image paths on an outfit into
// absolute URLs so RN <Image> can fetch them directly. Mirrors the same
// transform applied to wardrobe items, just extended to panelUrl/backgroundUrl.
const hydrateOutfitUrls = (outfit: Outfit): Outfit => ({
  ...outfit,
  panelUrl: toAbsoluteImageURL(outfit.panelUrl) || undefined,
  backgroundUrl: toAbsoluteImageURL(outfit.backgroundUrl) || undefined,
});
