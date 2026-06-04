import type { IBrandsRepository } from '@/src/domain';

const MOCK_BRANDS = ['Nike', 'Adidas', 'Zara', 'H&M', 'Gucci', 'Prada', "Levi's", 'Uniqlo'];

export class MockBrandsRepository implements IBrandsRepository {
  private saved = new Set<string>();

  async saveBrand(name: string): Promise<void> {
    this.saved.add(name);
  }

  async searchBrands(query: string): Promise<string[]> {
    const q = query.toLowerCase();
    return [...MOCK_BRANDS, ...this.saved].filter(b => b.toLowerCase().includes(q));
  }
}
