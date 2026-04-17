import type { IMoodBoardRepository, Outfit, SavedMoodBoard } from '@/src/domain';
import { apiClient } from '@/src/data/api/client';

export class ApiMoodBoardRepository implements IMoodBoardRepository {
  async save(outfit: Outfit, date?: string): Promise<SavedMoodBoard> {
    return apiClient.post<SavedMoodBoard>('/v1/moodboards', {
      outfit,
      date: date ?? new Date().toISOString().split('T')[0],
    });
  }

  async list(): Promise<SavedMoodBoard[]> {
    const response = await apiClient.get<{ moodboards: SavedMoodBoard[] }>('/v1/moodboards');
    return response.moodboards ?? [];
  }
}
