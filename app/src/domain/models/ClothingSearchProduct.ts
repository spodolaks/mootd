/** One product returned by the brand clothing-search service. */
export interface ClothingSearchProduct {
  id: string;
  title: string;
  /** Retailer / store name, e.g. "YOOX", "Alexander McQueen" */
  source: string;
  /** Pre-formatted price string, e.g. "$166.00" */
  price?: string;
  imageUrl: string;
}
