/** Snapshot of a wardrobe item at the time a moodboard was saved. */
export interface OutfitItem {
  id: string;
  category: string;
  label: string;
  imageUrl: string;
  pngImageUrl?: string;
}

/** Visual role assigned to an item inside an outfit collage. */
export type OutfitLayoutRole = 'hero' | 'support' | 'accent';

/** Per-item visual weight tag (P1-H). Orthogonal to layoutRole — a hero can
 *  also be a statement, but an accent can carry "statement" to boost a
 *  signature bag above a plain belt. */
export type OutfitVisualWeight = 'statement' | 'supporting' | 'minor';

/** Weather context the outfit was generated for — used to render a chip on the card. */
export interface OutfitWeather {
  temperature?: string;
  condition?: string;
  unit?: string;
}

/** A suggested outfit composed of wardrobe item IDs. */
export interface Outfit {
  /** Client-assigned ID used to correlate rating / swap feedback events with
   *  a specific outfit in a generated batch. Generated locally at receive
   *  time (see MoodBoardScreen#handleGeneratePress); the backend treats it
   *  as opaque. Optional so legacy data without IDs still deserialises. */
  id?: string;
  name: string;
  description: string;
  /** Wardrobe item IDs that make up the outfit (tops, bottoms, shoes, accessories). */
  items: string[];
  /** 1-line stylist explanation tying the outfit to the user's archetype + weather. */
  rationale?: string;
  /** Per-item visual role used by the collage to size and layer items. */
  layoutRoles?: Record<string, OutfitLayoutRole>;
  /** Per-item visual weight — marks the signature piece for boosted size
   *  treatment in the collage. The LLM sets exactly one "statement" entry
   *  per outfit; absence of the field degrades gracefully (all items
   *  render at the base size derived from layoutRoles + zone weight). */
  visualWeights?: Record<string, OutfitVisualWeight>;
  /** Item snapshots resolved at save time — used for display when items may have been deleted. */
  snapshots?: OutfitItem[];
  /** Text hints for complementary items not currently in the wardrobe. */
  suggestions?: string[];
  /** Per-outfit archetype alignment scores (archetype name → 0.0–1.0). */
  archetypeScores?: Record<string, number>;
  /** Archetype-driven item suggestion for small wardrobes (<20 items). */
  smartSuggestion?: string;
  /** Weather the outfit was generated for — rendered as a context chip. */
  weather?: OutfitWeather;
  /** Dominant colors per item as #RRGGBB hex, up to 4, deduped — rendered as a palette strip. */
  palette?: string[];
  /** Backend-chosen panel surface ID (e.g. "panel-2"). Debug/observability only — the frontend renders via panelUrl. */
  panelId?: string;
  /** Backend-chosen background surface ID. Debug/observability only. */
  backgroundId?: string;
  /** Resolved URL for the panel texture image (the surface garments sit on). */
  panelUrl?: string;
  /** Resolved URL for the ambient background image behind the panel. */
  backgroundUrl?: string;
}
