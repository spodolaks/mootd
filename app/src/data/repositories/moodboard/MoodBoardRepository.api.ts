import type { IMoodBoardRepository, Outfit, SaveOptions, SavedMoodBoard } from '@/src/domain';
import { apiClient, getApiBaseURL } from '@/src/data/api/client';

const toAbsoluteImageURL = (imageUrl: string | undefined): string => {
  if (!imageUrl) return '';
  if (imageUrl.startsWith('http://') || imageUrl.startsWith('https://')) return imageUrl;
  return `${getApiBaseURL()}${imageUrl}`;
};

const hydrateSavedBoard = (board: SavedMoodBoard): SavedMoodBoard => ({
  ...board,
  imageUrl: toAbsoluteImageURL(board.imageUrl) || undefined,
  outfit: {
    ...board.outfit,
    panelUrl: toAbsoluteImageURL(board.outfit.panelUrl) || undefined,
    backgroundUrl: toAbsoluteImageURL(board.outfit.backgroundUrl) || undefined,
    snapshots: board.outfit.snapshots?.map(snapshot => ({
      ...snapshot,
      imageUrl: toAbsoluteImageURL(snapshot.imageUrl),
      pngImageUrl: toAbsoluteImageURL(snapshot.pngImageUrl) || undefined,
    })),
  },
});

/** Accepts either a bare date string (legacy calling convention) or a full
 *  SaveOptions object. Having both avoids breaking callers who only need the
 *  date, while letting newer call sites forward generatedBatch + jobId so
 *  the server-side feedback emit can preserve the full generation trail. */
const normaliseOptions = (options?: string | SaveOptions): SaveOptions =>
  typeof options === 'string' ? { date: options } : (options ?? {});

/** Drop fields the wire schema doesn't know about. The FE's Outfit type
 *  carries `itemSnapshots` (the resolved-at-generation list with the
 *  `source` tag for owned vs filler items), but the backend's Outfit
 *  has no matching JSON tag — its strict decoder rejects the unknown
 *  key with a 400. Snapshots aren't actually shipped on save either
 *  way: the handler overwrites outfit.snapshots from the user's
 *  wardrobe before persisting (see moodboard/handler.go), so stripping
 *  these client-only fields is purely cosmetic for the request body. */
const toWirePayload = ({ itemSnapshots: _itemSnapshots, ...rest }: Outfit): Outfit => rest;

export class ApiMoodBoardRepository implements IMoodBoardRepository {
  async save(outfit: Outfit, options?: string | SaveOptions): Promise<SavedMoodBoard> {
    const opts = normaliseOptions(options);
    const board = await apiClient.post<SavedMoodBoard>('/v1/moodboards', {
      outfit: toWirePayload(outfit),
      date: opts.date ?? new Date().toISOString().split('T')[0],
      // Send only when present so older server deploys (pre-#8) that reject
      // unknown fields don't 400 on the absence of the schema — the decoder
      // skips JSON keys with missing values.
      ...(opts.generatedBatch && opts.generatedBatch.length > 0
        ? { generatedBatch: opts.generatedBatch.map(toWirePayload) }
        : {}),
      ...(opts.jobId ? { jobId: opts.jobId } : {}),
      ...(opts.boardImage ? { boardImage: opts.boardImage } : {}),
    });
    return hydrateSavedBoard(board);
  }

  async list(): Promise<SavedMoodBoard[]> {
    const response = await apiClient.get<{ moodboards: SavedMoodBoard[] }>('/v1/moodboards');
    return (response.moodboards ?? []).map(hydrateSavedBoard);
  }
}
