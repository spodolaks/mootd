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
  createdAt: string;
}
