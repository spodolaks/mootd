import type {
  ClothingSearchProduct,
  Outfit,
  ClothingDetectionResult,
  DetectedClothingItem,
  IWardrobeRepository,
  WardrobeItem,
} from '@/src/domain';
import type { OutfitProgress } from '@/src/domain/interfaces/IWardrobeRepository';
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

  async submitDetection(imageUri: string): Promise<string> {
    // Mirrors detectClothing's multipart handling but targets the async
    // endpoint. Returns as soon as the backend has queued the job —
    // typically <500 ms regardless of how long the actual detection takes.
    console.log('[Wardrobe] Submitting async detection job');

    const formData = new FormData();
    if (Platform.OS === 'web') {
      const res = await fetch(imageUri);
      const blob = await res.blob();
      formData.append('image', blob, 'photo.jpg');
    } else {
      formData.append('image', {
        uri: imageUri,
        name: 'photo.jpg',
        type: 'image/jpeg',
      } as unknown as Blob);
    }

    const response = await fetch(`${getApiBaseURL()}/v1/wardrobe/detect-jobs`, {
      method: 'POST',
      body: formData,
      headers: {
        ...(getAuthToken() ? { Authorization: `Bearer ${getAuthToken()}` } : {}),
      },
    });

    const body = (await response.json().catch(() => ({}))) as Record<string, unknown>;
    if (!response.ok) {
      const msg =
        typeof body.error === 'string' ? body.error : `Detection submit failed (${response.status})`;
      throw new ApiError(msg, response.status, body);
    }

    const jobId = body.jobId;
    if (typeof jobId !== 'string' || !jobId) {
      throw new ApiError('Detection submit returned no jobId', response.status, body);
    }
    console.log('[Wardrobe] ✓ Detection job accepted:', jobId);
    return jobId;
  }

  async pollDetectionJob(jobId: string): Promise<{
    status: 'pending' | 'processing' | 'completed' | 'failed';
    items?: ClothingDetectionResult['items'];
    error?: string;
  }> {
    // The backend returns the full DetectJob shape; we project it into the
    // narrower interface the store expects. Items come back with relative
    // imageUrl/pngImageUrl — hydrate them to absolute here so callers get
    // the same shape the sync detectClothing path produces.
    const raw = await apiClient.get<{
      status: 'pending' | 'processing' | 'completed' | 'failed';
      items?: DetectAPIResponse['items'];
      error?: string;
    }>(`/v1/wardrobe/detect-jobs/${encodeURIComponent(jobId)}`);

    const items: DetectedClothingItem[] | undefined = raw.items?.map(item => ({
      id: item.id,
      category: item.category,
      label: item.label,
      confidence: item.confidence,
      imageUrl: toAbsoluteImageURL(item.imageUrl),
      pngImageUrl: toAbsoluteImageURL(item.pngImageUrl) || undefined,
      traits: item.traits,
    }));

    return { status: raw.status, items, error: raw.error };
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

  async submitOutfitGeneration(
    weather?: { temperature: number; condition: string; unit: string },
    idempotencyKey?: string,
  ): Promise<string> {
    const params = new URLSearchParams();
    if (weather) {
      params.set('temperature', String(Math.round(weather.temperature)));
      params.set('condition', weather.condition);
      params.set('unit', weather.unit === 'fahrenheit' ? 'F' : 'C');
    }
    const qs = params.toString();
    const url = `/v1/outfits/generate${qs ? `?${qs}` : ''}`;
    console.log(`[Wardrobe] → POST ${url}`);
    // mootd#42 — Idempotency-Key dedupes double-taps + network
    // retries. Backend looks the key up in Redis with a 60s
    // TTL; a duplicate inside the window returns the original
    // jobID instead of starting a second (paid) generation.
    const init: RequestInit = idempotencyKey
      ? { headers: { 'Idempotency-Key': idempotencyKey } }
      : {};
    const response = await apiClient.post<{ jobId: string }>(url, {}, init);
    console.log(`[Wardrobe] ✓ Job submitted: ${response.jobId}`);
    return response.jobId;
  }

  // mootd#62 — SSE streaming variant. Opens an SSE connection
  // to /v1/outfits/generate (Accept: text/event-stream); fires
  // onProgress per server event with the cumulative shape;
  // resolves with the final outfits at `event: done`. The
  // backend bridges per-stage GenerateProgress events
  // (connecting → streaming heartbeats → done) into the SSE
  // response so we don't need a separate poll loop.
  //
  // SSE parser is hand-rolled (split on blank-line boundaries,
  // parse `event:` + `data:` fields). RN's fetch supports
  // body.getReader() since 0.71.
  async streamOutfitGeneration(
    onProgress: (p: OutfitProgress) => void,
    weather?: { temperature: number; condition: string; unit: string },
    _idempotencyKey?: string,
  ): Promise<Outfit[]> {
    const params = new URLSearchParams();
    if (weather) {
      params.set('temperature', String(Math.round(weather.temperature)));
      params.set('condition', weather.condition);
      params.set('unit', weather.unit === 'fahrenheit' ? 'F' : 'C');
    }
    const qs = params.toString();
    const path = `/v1/outfits/generate${qs ? `?${qs}` : ''}`;
    const baseURL = getApiBaseURL();
    const token = getAuthToken();
    console.log(`[Wardrobe] → POST(stream) ${path}`);

    const res = await fetch(`${baseURL}${path}`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Accept': 'text/event-stream',
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      body: JSON.stringify({}),
    });
    if (!res.ok || !res.body) {
      throw new Error(`stream open failed: HTTP ${res.status}`);
    }

    const reader = res.body.getReader();
    const decoder = new TextDecoder('utf-8');
    let buffer = '';
    let final: Outfit[] | null = null;
    let lastError: string | null = null;

    const parseEvent = (block: string): { event: string; data: string } | null => {
      let event = 'message';
      const dataLines: string[] = [];
      for (const line of block.split('\n')) {
        if (line.startsWith('event:')) {
          event = line.slice(6).trim();
        } else if (line.startsWith('data:')) {
          dataLines.push(line.slice(5).trim());
        }
      }
      if (dataLines.length === 0) return null;
      return { event, data: dataLines.join('\n') };
    };

    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });

      let idx;
      while ((idx = buffer.indexOf('\n\n')) !== -1) {
        const block = buffer.slice(0, idx);
        buffer = buffer.slice(idx + 2);
        const parsed = parseEvent(block);
        if (!parsed) continue;
        try {
          const payload = JSON.parse(parsed.data);
          const stage: OutfitProgress['stage'] = parsed.event === 'done'
            ? 'done'
            : parsed.event === 'error'
              ? 'error'
              : 'streaming';
          const outfits = payload.outfits
            ? (payload.outfits as Outfit[]).map(hydrateOutfitUrls)
            : undefined;
          onProgress({
            stage,
            outfits,
            description: payload.description,
          });
          if (parsed.event === 'done' && outfits) {
            final = outfits;
          } else if (parsed.event === 'error') {
            lastError = String(payload.description ?? 'streaming failed');
          }
        } catch (err) {
          console.warn(`[Wardrobe] SSE parse error: ${err}`);
        }
      }
    }

    if (final) return final;
    throw new Error(lastError ?? 'stream ended without final outfits');
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
