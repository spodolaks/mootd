import type { Outfit } from './Outfit';

/**
 * A moodboard selected by the user for a specific date, persisted on the backend.
 */
export interface SavedMoodBoard {
  id: string;
  userId: string;
  outfit: Outfit;
  /** ISO date string, YYYY-MM-DD */
  date: string;
  /** Optional path to the rendered collage PNG captured at save time.
   *  Undefined on legacy rows (saved before the render-capture feature) —
   *  callers should fall back to rendering from outfit.snapshots. */
  imageUrl?: string;
  createdAt: string;
}
