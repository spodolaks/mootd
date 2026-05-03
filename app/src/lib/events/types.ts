// TypeScript types for the analytics-event catalog
// (P2-01 / mootd-admin#18). One discriminated union per event;
// callers can only emit what's declared, and properties are
// strongly typed per event name.
//
// Source of truth: mootd-contracts/events/schema.md. When the
// catalog grows, add to both this file and the backend's
// `CatalogNames` allowlist (mootd/backend/internal/events/domain.go).
//
// Why a discriminated union vs. a single `Record<EventName,
// Record<string, unknown>>`?
//   - Catches typos at compile time: `events.emit('opened_app',
//     {})` is a TS error.
//   - Catches property-name typos too: emitting `app_opened`
//     with `{platorm: 'ios'}` fails TS.
//   - Makes adding a new event a single coordinated change
//     rather than two unrelated places that can drift.

// ─── Core ────────────────────────────────────────────────────

export type EventAppOpened = {
  name: 'app_opened';
  properties: {
    platform: 'ios' | 'android' | 'web';
    appVersion: string;
    sessionType: 'cold' | 'warm';
  };
};

export type EventScreenView = {
  name: 'screen_view';
  properties: {
    screen:
      | 'moodboard'
      | 'wardrobe'
      | 'calendar'
      | 'profile'
      | 'build_wardrobe'
      | 'item_details'
      | 'detected_item'
      | 'trait_selection';
    fromScreen?: string;
  };
};

export type EventSessionStart = {
  name: 'session_start';
  properties: {
    platform: 'ios' | 'android' | 'web';
  };
};

export type EventSessionHeartbeat = {
  name: 'session_heartbeat';
  properties: {
    /** Wall-clock seconds since session start. Lets the server
     *  reconstruct duration even when session_end is dropped
     *  (force-close, OS kill, etc.). */
    elapsedSec: number;
  };
};

export type EventSessionEnd = {
  name: 'session_end';
  properties: {
    durationMs: number;
    screensVisited: number;
    featuresUsed: string[];
  };
};

// ─── Wardrobe ────────────────────────────────────────────────

export type EventPhotoUploaded = {
  name: 'photo_uploaded';
  properties: {
    source: 'camera' | 'library';
    sizeKb: number;
    traceId?: string;
  };
};

export type EventItemsDetected = {
  name: 'items_detected';
  properties: {
    itemCount: number;
    durationMs: number;
    traceId?: string;
  };
};

export type EventItemConfirmed = {
  name: 'item_confirmed';
  properties: {
    itemId: string;
    category: string;
  };
};

export type EventItemRejected = {
  name: 'item_rejected';
  properties: {
    itemId: string;
    reason?: string;
  };
};

// ─── Outfit ──────────────────────────────────────────────────

export type EventGenerateOutfitRequested = {
  name: 'generate_outfit_requested';
  properties: {
    /** Wardrobe items present at request time — count, NOT IDs.
     *  IDs are PII-adjacent; counts are not. */
    wardrobeItemCount: number;
    weather?: 'hot' | 'warm' | 'mild' | 'cool' | 'cold';
  };
};

export type EventGeneratedOutfit = {
  name: 'generated_outfit';
  properties: {
    outfitCount: number;
    durationMs: number;
    traceId?: string;
  };
};

export type EventViewedOutfit = {
  name: 'viewed_outfit';
  properties: {
    outfitIndex: number;
    fromScreen?: string;
  };
};

export type EventRatedOutfit = {
  name: 'rated_outfit';
  properties: {
    rating: 1 | 2 | 3 | 4 | 5;
    outfitIndex: number;
  };
};

export type EventSwappedItem = {
  name: 'swapped_item';
  properties: {
    fromItemId: string;
    toItemId: string;
  };
};

// ─── Moodboard / sharing ─────────────────────────────────────

export type EventSavedMoodboard = {
  name: 'saved_moodboard';
  properties: {
    outfitIndex: number;
    /** Date the moodboard was saved against (YYYY-MM-DD).
     *  Used for cohort analyses against scheduled outfits. */
    forDate?: string;
  };
};

export type EventViewedCalendarDate = {
  name: 'viewed_calendar_date';
  properties: {
    date: string; // YYYY-MM-DD
    /** True when the date already had a saved moodboard. */
    hasMoodboard: boolean;
  };
};

export type EventSharedMoodboard = {
  name: 'shared_moodboard';
  properties: {
    target: 'instagram' | 'message' | 'system' | 'copy_link';
  };
};

// ─── Auth ────────────────────────────────────────────────────

export type EventSignedUp = {
  name: 'signed_up';
  properties: {
    method: 'google' | 'apple' | 'email';
  };
};

export type EventSignedIn = {
  name: 'signed_in';
  properties: {
    method: 'google' | 'apple' | 'email';
  };
};

export type EventSignedOut = {
  name: 'signed_out';
  properties: Record<string, never>;
};

// ─── Discriminated union ─────────────────────────────────────

/** Every emittable event. The compiler enforces (name,
 *  properties) consistency at every call site. */
export type AnyEvent =
  | EventAppOpened
  | EventScreenView
  | EventSessionStart
  | EventSessionHeartbeat
  | EventSessionEnd
  | EventPhotoUploaded
  | EventItemsDetected
  | EventItemConfirmed
  | EventItemRejected
  | EventGenerateOutfitRequested
  | EventGeneratedOutfit
  | EventViewedOutfit
  | EventRatedOutfit
  | EventSwappedItem
  | EventSavedMoodboard
  | EventViewedCalendarDate
  | EventSharedMoodboard
  | EventSignedUp
  | EventSignedIn
  | EventSignedOut;

/** Tagged-union name extraction. Use as
 *  `events.emit<'app_opened'>('app_opened', { … })` when TS
 *  needs an explicit hint, but the discriminated union usually
 *  infers name + properties correctly without it. */
export type EventName = AnyEvent['name'];

/** Get the properties type for a given event name. */
export type PropertiesFor<N extends EventName> = Extract<
  AnyEvent,
  { name: N }
>['properties'];
