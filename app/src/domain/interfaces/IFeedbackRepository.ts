import type { Outfit } from '../models/Outfit';

/** Verb describing how the user reacted to a generated outfit batch.
 *  Mirrored server-side; keep values lowercase-with-underscores and in sync
 *  with backend/internal/feedback/domain.go's Action enum. */
export type FeedbackAction =
  | 'saved'
  | 'skipped'
  | 'regenerated'
  | 'rated'
  | 'item_swapped';

/** Coarse, non-PII context shipped with every event so the training pipeline
 *  can segment by taste conditions later. All fields optional — emit what
 *  the client has, leave the rest blank. */
export interface FeedbackContext {
  weather?: string;
  dayOfWeek?: string;
  hour?: number;
  archetype?: string;
  occasion?: string;
}

/** Single outfit entry inside a generation batch, trimmed to the fields the
 *  training job needs. Kept separate from the full Outfit type so large
 *  display-only fields (panel URLs, palettes) aren't shipped on every event. */
export interface FeedbackOutfitSnapshot {
  id?: string;
  name?: string;
  items: string[];
  rationale?: string;
  archetypeScores?: Record<string, number>;
}

/** POST /v1/outfits/feedback request body. */
export interface FeedbackSubmitRequest {
  action: FeedbackAction;
  /** Optional job ID tying the event back to the generate call that produced
   *  the batch. Useful when a single job produces multiple events over time
   *  (e.g. swap → rate → save). */
  jobId?: string;
  /** The ID of the outfit the user is reacting to (typically the one that
   *  was visible on-screen). Optional for events like "regenerated" that
   *  apply to the whole batch. */
  chosenOutfitId?: string;
  /** 1–5 rating. Required when action === 'rated'. */
  rating?: number;
  /** Full generated batch. Preserving the rejected members is what makes
   *  preference-pair / DPO training possible later. Optional for events
   *  where the batch isn't relevant. */
  generatedBatch?: FeedbackOutfitSnapshot[];
  context?: FeedbackContext;
  /** Populated only for action === 'item_swapped'. The wardrobe item IDs
   *  involved in the swap: swappedFrom is the item the user removed,
   *  swappedTo is the one they picked. Stored explicitly so training
   *  doesn't have to diff sequential generatedBatch snapshots to recover
   *  the (rejected → accepted) pair. */
  swappedFrom?: string;
  swappedTo?: string;
}

export interface IFeedbackRepository {
  submit: (req: FeedbackSubmitRequest) => Promise<void>;
}

/** Helper: trim an Outfit to the shape the feedback pipeline stores. */
export const outfitToSnapshot = (o: Outfit): FeedbackOutfitSnapshot => ({
  id: o.id,
  name: o.name,
  items: o.items,
  rationale: o.rationale,
  archetypeScores: o.archetypeScores,
});

/** Helper: derive the top archetype name from an outfit's scores, for
 *  feedback context. Returns undefined when there are no scores. */
export const topArchetypeOf = (o: Outfit): string | undefined => {
  if (!o.archetypeScores) return undefined;
  let top: string | undefined;
  let topScore = -Infinity;
  for (const [name, score] of Object.entries(o.archetypeScores)) {
    if (score > topScore) {
      top = name;
      topScore = score;
    }
  }
  return top;
};

/** Helper: condensed weather string for feedback context.
 *  e.g. {condition: "sunny", temperature: "18", unit: "C"} → "sunny 18C" */
export const weatherContextString = (o: Outfit): string | undefined => {
  if (!o.weather) return undefined;
  const parts: string[] = [];
  if (o.weather.condition) parts.push(o.weather.condition);
  if (o.weather.temperature) {
    parts.push(`${o.weather.temperature}${o.weather.unit || 'C'}`);
  }
  return parts.length > 0 ? parts.join(' ') : undefined;
};
