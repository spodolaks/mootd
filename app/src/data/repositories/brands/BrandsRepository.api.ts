import type { IBrandsRepository } from '@/src/domain';
import { apiClient } from '@/src/data/api/client';

export class ApiBrandsRepository implements IBrandsRepository {
  async saveBrand(name: string): Promise<void> {
    await apiClient.post('/v1/brands', { name });
  }

  async searchBrands(query: string): Promise<string[]> {
    const response = await apiClient.get<{ brands: string[] }>(`/v1/brands?q=${encodeURIComponent(query)}`);
    return response.brands ?? [];
  }
}
