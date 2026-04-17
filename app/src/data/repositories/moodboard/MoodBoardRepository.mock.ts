import type { IMoodBoardRepository, Outfit, SavedMoodBoard } from '@/src/domain';

export class MockMoodBoardRepository implements IMoodBoardRepository {
  private boards: SavedMoodBoard[] = [];

  async save(outfit: Outfit, date?: string): Promise<SavedMoodBoard> {
    const board: SavedMoodBoard = {
      id: Math.random().toString(36).slice(2),
      userId: 'mock_user',
      outfit,
      date: date ?? new Date().toISOString().split('T')[0],
      createdAt: new Date().toISOString(),
    };
    this.boards.unshift(board);
    return board;
  }

  async list(): Promise<SavedMoodBoard[]> {
    return this.boards;
  }
}
