export interface IBrandsRepository {
  /** Persist a brand name to the global dictionary. Silently ignored if empty. */
  saveBrand(name: string): Promise<void>;
  /** Return brand display names that contain the query string (case-insensitive). */
  searchBrands(query: string): Promise<string[]>;
}
