import type { Outfit, SavedMoodBoard } from '../models';

export interface IMoodBoardRepository {
  /** Save the selected outfit as a moodboard for the given date (default: today). */
  save(outfit: Outfit, date?: string): Promise<SavedMoodBoard>;
  /** Fetch all saved moodboards for the user, newest first. */
  list(): Promise<SavedMoodBoard[]>;
}
