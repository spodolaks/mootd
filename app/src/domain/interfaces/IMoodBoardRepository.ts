import type { Outfit, SavedMoodBoard } from '../models';

/** Optional context forwarded on save so the server-side feedback emit can
 *  preserve the full generation trail. generatedBatch is the list of outfits
 *  the user was shown (saved + rejected) — its presence is what makes
 *  preference-pair / DPO training possible later. jobId ties the save back
 *  to the POST /v1/outfits/generate call that produced the batch. */
export interface SaveOptions {
  date?: string;
  generatedBatch?: Outfit[];
  jobId?: string;
  /** Base64-encoded PNG of the rendered collage, captured on-device before
   *  save. Optional — servers that don't receive it will skip GridFS storage
   *  and the calendar will render from outfit.snapshots as a fallback.
   *  Accepts either a raw base64 string or a `data:image/png;base64,…` URL. */
  boardImage?: string;
}

export interface IMoodBoardRepository {
  /** Save the selected outfit as a moodboard. Date defaults to today.
   *  Accepts either a bare date string (legacy) or a SaveOptions object. */
  save(outfit: Outfit, options?: string | SaveOptions): Promise<SavedMoodBoard>;
  /** Fetch all saved moodboards for the user, newest first. */
  list(): Promise<SavedMoodBoard[]>;
}
