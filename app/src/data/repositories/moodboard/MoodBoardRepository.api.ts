import type { IMoodBoardRepository, Outfit, SaveOptions, SavedMoodBoard } from '@/src/domain';
import { apiClient } from '@/src/data/api/client';

/** Accepts either a bare date string (legacy calling convention) or a full
 *  SaveOptions object. Having both avoids breaking callers who only need the
 *  date, while letting newer call sites forward generatedBatch + jobId so
 *  the server-side feedback emit can preserve the full generation trail. */
const normaliseOptions = (options?: string | SaveOptions): SaveOptions =>
  typeof options === 'string' ? { date: options } : options ?? {};

export class ApiMoodBoardRepository implements IMoodBoardRepository {
  async save(outfit: Outfit, options?: string | SaveOptions): Promise<SavedMoodBoard> {
    const opts = normaliseOptions(options);
    return apiClient.post<SavedMoodBoard>('/v1/moodboards', {
      outfit,
      date: opts.date ?? new Date().toISOString().split('T')[0],
      // Send only when present so older server deploys (pre-#8) that reject
      // unknown fields don't 400 on the absence of the schema — the decoder
      // skips JSON keys with missing values.
      ...(opts.generatedBatch && opts.generatedBatch.length > 0
        ? { generatedBatch: opts.generatedBatch }
        : {}),
      ...(opts.jobId ? { jobId: opts.jobId } : {}),
    });
  }

  async list(): Promise<SavedMoodBoard[]> {
    const response = await apiClient.get<{ moodboards: SavedMoodBoard[] }>('/v1/moodboards');
    return response.moodboards ?? [];
  }
}
