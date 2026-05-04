import type {
  ClothingSearchProduct,
  ClothingDetectionResult,
  Outfit,
  IWardrobeRepository,
  WardrobeItem,
} from '@/src/domain';

/**
 * Mock implementation that simulates the clothing detection API.
 * Returns realistic-looking results after an artificial delay.
 */
export class MockWardrobeRepository implements IWardrobeRepository {
  private delay(ms = 1500): Promise<void> {
    return new Promise(resolve => setTimeout(resolve, ms));
  }

  private mockItems = [
    { id: 'det_001', category: 'blazer', label: 'Slim Fit Blazer', confidence: 0.94 },
    { id: 'det_002', category: 'shirt', label: 'Oxford Shirt', confidence: 0.89 },
    { id: 'det_003', category: 'pants', label: 'Chinos', confidence: 0.91 },
  ];

  async detectClothing(imageUri: string): Promise<ClothingDetectionResult> {
    await this.delay();
    return { originalImageUri: imageUri, items: this.mockItems };
  }

  // Async-path mock: the store posts then polls, so the mock hands back
  // a job ID immediately and returns a completed status on first poll.
  // That gives the UI the same polling shape as production without
  // introducing fake wait-states the test harness would have to stub.
  private mockJobs = new Map<string, 'completed'>();

  async submitDetection(imageUri: string): Promise<string> {
    await this.delay(200);
    const jobId = `mock_job_${Date.now()}`;
    this.mockJobs.set(jobId, 'completed');
    // Keep imageUri for API-parity logging; mock doesn't actually use it.
    void imageUri;
    return jobId;
  }

  async pollDetectionJob(jobId: string): Promise<{
    status: 'pending' | 'processing' | 'completed' | 'failed';
    items?: ClothingDetectionResult['items'];
    error?: string;
  }> {
    await this.delay(100);
    if (!this.mockJobs.has(jobId)) {
      return { status: 'failed', error: 'unknown job' };
    }
    return { status: 'completed', items: this.mockItems };
  }

  async updateItem(_id: string, _traits: Record<string, string>, _label?: string, _imageUrl?: string): Promise<void> {
    await this.delay(300);
  }

  async deleteItem(_id: string): Promise<void> {
    await this.delay(300);
  }

  async getItems(_params?: { limit?: number; cursor?: string }): Promise<{ items: WardrobeItem[]; nextCursor: string | null }> {
    await this.delay(500);
    return {
      items: [
        {
          id: 'item_001',
          userId: 'mock_user',
          category: 'outerwear',
          label: 'Slim Fit Blazer',
          imageUrl: '',
          traits: { fit: 'slim', color: 'navy' },
          createdAt: new Date().toISOString(),
        },
        {
          id: 'item_002',
          userId: 'mock_user',
          category: 'tops',
          label: 'Oxford Shirt',
          imageUrl: '',
          traits: { material: 'cotton', color: 'white' },
          createdAt: new Date().toISOString(),
        },
        {
          id: 'item_003',
          userId: 'mock_user',
          category: 'bottoms',
          label: 'Chinos',
          imageUrl: '',
          traits: { fit: 'slim', color: 'beige' },
          createdAt: new Date().toISOString(),
        },
      ],
      nextCursor: null,
    };
  }

  async submitOutfitGeneration(
    _weather?: { temperature: number; condition: string; unit: string },
    _idempotencyKey?: string,
  ): Promise<string> {
    await this.delay(500);
    return 'mock_job_' + Date.now();
  }

  async pollOutfitJob(_jobId: string): Promise<{ status: 'pending' | 'processing' | 'completed' | 'failed'; outfits?: Outfit[]; error?: string }> {
    await this.delay(2000);
    return { status: 'completed', outfits: [] };
  }

  async searchByBrand(_itemId: string, _brand: string): Promise<ClothingSearchProduct[]> {
    return [];
  }

  async getOutfits(_weather?: { temperature: number; condition: string; unit: string }): Promise<Outfit[]> {
    await this.delay(300);
    return [];
  }
}
