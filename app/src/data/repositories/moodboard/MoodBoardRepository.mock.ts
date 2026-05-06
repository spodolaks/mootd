import type { IMoodBoardRepository, Outfit, SaveOptions, SavedMoodBoard } from '@/src/domain';

const resolveDate = (options?: string | SaveOptions): string | undefined =>
  typeof options === 'string' ? options : options?.date;

export class MockMoodBoardRepository implements IMoodBoardRepository {
  private boards: SavedMoodBoard[] = [];

  async save(outfit: Outfit, options?: string | SaveOptions): Promise<SavedMoodBoard> {
    const board: SavedMoodBoard = {
      id: Math.random().toString(36).slice(2),
      userId: 'mock_user',
      outfit,
      date: resolveDate(options) ?? new Date().toISOString().split('T')[0],
      createdAt: new Date().toISOString(),
    };
    this.boards.unshift(board);
    // The mock doesn't persist generatedBatch / jobId — the shape is defined
    // server-side and only matters for real feedback collection. Log in dev
    // so the offline flow still reveals what's being forwarded.
    if (typeof options === 'object' && options?.generatedBatch) {
       
      console.log(
        '[mock moodboard] save received batch size',
        options.generatedBatch.length,
        'jobId=',
        options.jobId,
      );
    }
    return board;
  }

  async list(): Promise<SavedMoodBoard[]> {
    return this.boards;
  }
}
