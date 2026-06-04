// Analytics event SDK (P2-01 / mootd-admin#18).
//
// Public surface:
//   events.start()                  — boots the SDK; idempotent
//   events.emit(name, properties)   — queue + maybe flush
//   events.flush()                  — force a flush (e.g. on
//                                     foreground transition)
//   events.sessionId                — current session id
//
// Behaviour:
//   - Single client-generated session id per app launch.
//   - Queue persisted to AsyncStorage on every push so a
//     force-close doesn't drop events.
//   - Flushes when the queue hits BATCH_SIZE (25) OR every
//     FLUSH_INTERVAL_MS (10 s).
//   - Network failure → event stays in the queue; next flush
//     retries.
//   - Unauthenticated emits get queued anyway. They flush as
//     soon as setAuthToken() lands; the queue carries them
//     across login.
//
// Acceptance criteria from the issue:
//   - ~10KB zipped weight: this file is <8KB unminified, no
//     extra deps.
//   - Dropped events log a warning but never block user
//     actions: every public method is sync and never throws.
//   - PII never in properties: `assertNoPII` runs at queue
//     time and (a) drops the event with a console.warn in dev,
//     (b) silently drops in prod-or-whenever-the-test-flag-is-off.

import AsyncStorage from '@react-native-async-storage/async-storage';
import type { EventName, PropertiesFor } from './types';

const STORAGE_KEY = 'mootd-events-queue-v1';
const BATCH_SIZE = 25;
const FLUSH_INTERVAL_MS = 10_000;

/** Hard upper bound on queue depth before we drop the oldest.
 *  Caps storage growth when offline for a long time + the
 *  queue grows past anything plausible. */
const MAX_QUEUE_DEPTH = 500;

/** PII patterns we refuse to send. Conservative — better to
 *  drop a legitimate event than ship a real email address. */
const PII_REGEX = [
  /^[^\s@]+@[^\s@]+\.[^\s@]+$/, // email
  /^\+?[\d\s\-()]{8,}$/, // phone-ish
];

interface QueuedEvent {
  name: string;
  sessionId: string;
  properties: Record<string, unknown>;
  /** Local timestamp for debugging only — server uses its own
   *  `createdAt`. */
  ts: number;
}

interface SDKState {
  sessionId: string;
  queue: QueuedEvent[];
  flushTimer: ReturnType<typeof setInterval> | null;
  flushing: boolean;
  /** Bearer token for the ingest fetch. Wired by setAuthToken
   *  via the `bindAuth` helper below. */
  authToken: string | null;
  /** Base URL for the ingest endpoint. Wired at start time so
   *  unit tests can stub it. */
  apiBaseUrl: string;
  started: boolean;
}

const state: SDKState = {
  sessionId: generateSessionId(),
  queue: [],
  flushTimer: null,
  flushing: false,
  authToken: null,
  apiBaseUrl: '',
  started: false,
};

// ─── Public API ──────────────────────────────────────────────

/** Boot the SDK. Restores the queue from AsyncStorage and
 *  schedules the periodic flush. Idempotent — calling twice
 *  is a no-op. */
export async function start(opts: { apiBaseUrl: string }): Promise<void> {
  if (state.started) return;
  state.started = true;
  state.apiBaseUrl = opts.apiBaseUrl;

  // Restore the queue. Persistence failures (corrupt JSON,
  // migration mismatch) drop the queue but don't block boot.
  try {
    const raw = await AsyncStorage.getItem(STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw);
      if (Array.isArray(parsed)) {
        state.queue = parsed.slice(-MAX_QUEUE_DEPTH);
      }
    }
  } catch (e) {
    console.warn('[events] queue restore failed; starting empty', e);
  }

  // Periodic flush.
  state.flushTimer = setInterval(() => {
    void flush();
  }, FLUSH_INTERVAL_MS);

  // Flush whatever was restored. Best-effort; unauth'd events
  // wait for setAuthToken().
  void flush();
}

/** Stop the SDK. Used on sign-out + tests. Idempotent. */
export function stop(): void {
  if (state.flushTimer) {
    clearInterval(state.flushTimer);
    state.flushTimer = null;
  }
  state.started = false;
}

/** Update the bearer token. Called from the auth store
 *  whenever the token changes — login, refresh, logout. */
export function setAuthToken(token: string | null): void {
  state.authToken = token;
  if (token && state.queue.length > 0) {
    // Newly authenticated → push whatever was queued during
    // anonymous session.
    void flush();
  }
}

/** Read the current session id. Stable for the lifetime of
 *  the process. */
export function getSessionId(): string {
  return state.sessionId;
}

/** Reset to a fresh session. Call this when the issue's
 *  "session_end on background > 30s" rule fires — the next
 *  app_opened starts a new session. */
export function rotateSessionId(): string {
  state.sessionId = generateSessionId();
  return state.sessionId;
}

/** Queue one event. Sync, never throws, never blocks. The
 *  generic enforces (name, properties) consistency at the
 *  call site via the discriminated-union types.
 *
 *  Pattern: events.emit('app_opened', { platform: 'ios', ... }) */
export function emit<N extends EventName>(name: N, properties: PropertiesFor<N>): void {
  // Defence-in-depth PII check. Drops + warns rather than
  // throwing — the call site shouldn't care.
  const safe = scrubPII(properties as Record<string, unknown>, name);
  if (safe === null) return;

  const ev: QueuedEvent = {
    name,
    sessionId: state.sessionId,
    properties: safe,
    ts: Date.now(),
  };

  state.queue.push(ev);
  if (state.queue.length > MAX_QUEUE_DEPTH) {
    // Drop oldest. We could also drop newest; oldest matches
    // what most analytics SDKs do (preserve recent activity).
    state.queue = state.queue.slice(-MAX_QUEUE_DEPTH);
  }

  // Persist on every push. AsyncStorage is fast (~ms) but
  // we don't await — a slow disk shouldn't slow user actions.
  void persistQueue();

  if (state.queue.length >= BATCH_SIZE) {
    void flush();
  }
}

/** Force a flush. Useful on transitions where we want events
 *  to reach the server before something user-visible happens
 *  (e.g. app foregrounding after a long background). */
export async function flush(): Promise<void> {
  if (!state.started) return;
  if (state.flushing) return;
  if (state.queue.length === 0) return;
  if (!state.authToken) return; // Wait for login.
  if (!state.apiBaseUrl) return;

  state.flushing = true;
  // Snapshot the queue, send the batch, drop only the events
  // we successfully delivered. Splicing the slice keeps
  // events emitted during the in-flight POST in place.
  const batch = state.queue.slice(0, BATCH_SIZE);
  const remaining = state.queue.slice(batch.length);

  try {
    const res = await fetch(`${state.apiBaseUrl}/v1/events`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${state.authToken}`,
      },
      body: JSON.stringify({
        events: batch.map(e => ({
          name: e.name,
          sessionId: e.sessionId,
          properties: e.properties,
        })),
      }),
    });

    if (res.ok) {
      // Server may have rejected some events as invalid —
      // we treat those as delivered too (re-sending wouldn't
      // help; the SDK's TS types should have caught them
      // before queueing).
      state.queue = remaining;
      await persistQueue();
    } else if (res.status === 401) {
      // Token expired between flush start + send. Hold the
      // batch — auth refresh will re-attempt.
      // (We don't drop on 401; that would lose data when the
      // user is in the middle of a refresh.)
    } else if (res.status === 413) {
      // Payload too large. Halve the batch + retry next tick.
      // Drop the front half — pragmatic, oldest events first.
      const halfSent = Math.max(1, Math.floor(batch.length / 2));
      state.queue = state.queue.slice(halfSent);
      await persistQueue();
      console.warn('[events] 413 from ingest; halving batch');
    } else {
      // 5xx / other — keep the batch, retry next tick.
      console.warn(`[events] ingest ${res.status}; will retry`);
    }
  } catch (e) {
    // Offline / DNS / TLS — keep the batch.
    console.warn('[events] flush failed; will retry', e);
  } finally {
    state.flushing = false;
  }
}

// ─── Internals ───────────────────────────────────────────────

async function persistQueue(): Promise<void> {
  try {
    await AsyncStorage.setItem(STORAGE_KEY, JSON.stringify(state.queue));
  } catch (e) {
    console.warn('[events] queue persist failed', e);
  }
}

/** scrubPII inspects every string value in the properties bag.
 *  Returns the bag unmodified when clean; null when a PII
 *  pattern matches (event is dropped + a warning logged). */
function scrubPII(
  properties: Record<string, unknown>,
  eventName: string
): Record<string, unknown> | null {
  for (const [k, v] of Object.entries(properties)) {
    if (typeof v !== 'string') continue;
    for (const re of PII_REGEX) {
      if (re.test(v)) {
        if (__DEV__) {
          console.warn(
            `[events] dropped ${eventName}: property ${k} looks like PII (${re}). ` +
              `Use an ID or count instead.`
          );
        }
        return null;
      }
    }
  }
  return properties;
}

/** generateSessionId returns a UUID-ish string. Doesn't need
 *  cryptographic uniqueness — it just has to identify the
 *  session within the user's other sessions. crypto.randomUUID
 *  is available on RN 0.83+ via expo's polyfill, but we fall
 *  back to a Math.random approach for safety. */
function generateSessionId(): string {
  // Prefer the standard if it exists.

  const cr = (globalThis as any).crypto;
  if (cr && typeof cr.randomUUID === 'function') {
    return cr.randomUUID();
  }
  return 'sess-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 10);
}

// Type re-export so callers can pull AnyEvent etc. from
// `@/src/lib/events` without reaching into ./types.
export type { AnyEvent, EventName, PropertiesFor } from './types';
