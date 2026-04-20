import { Platform, Share } from 'react-native';

/**
 * Minimal branded-share helpers for Instagram and Facebook.
 *
 * Platform realities shaped this API:
 *  - Neither platform offers a web endpoint that directly posts an image
 *    with one click; Facebook has a sharer URL that accepts a link, and
 *    Instagram has no share web API at all.
 *  - Native iOS / Android have a system share sheet that surfaces both
 *    apps when installed; we can't force-target a specific app from JS.
 *
 * So we do the honest thing: on web, each button uses the best-effort
 * path for its platform; on native, both buttons invoke Share.share()
 * (the icon is a hint to the user, the system sheet does the routing).
 * Every helper is fire-and-forget — failures are logged, the calendar
 * keeps rendering.
 */

export type SharePlatform = 'instagram' | 'facebook';

export interface ShareTarget {
  /** Absolute URL to the rendered moodboard PNG. */
  imageUrl: string;
  /** Short caption the share sheet / native app can prefill. */
  caption?: string;
}

/** Open Facebook's web sharer with the given URL. Requires the URL to be
 *  publicly reachable; local-dev URLs (localhost:8089) will load but Facebook
 *  won't be able to fetch the image, so the share card shows no preview.
 *  Acceptable tradeoff for a local test; works end-to-end in prod. */
const openFacebookWebShare = (imageUrl: string): void => {
  const href = `https://www.facebook.com/sharer/sharer.php?u=${encodeURIComponent(imageUrl)}`;
  if (typeof window !== 'undefined') {
    window.open(href, '_blank', 'noopener,noreferrer');
  }
};

/** Download the PNG to the user's device and open instagram.com in a new
 *  tab. Instagram has no web share endpoint so the user finishes the flow
 *  by hand; the download + tab combo at least removes the "find the file"
 *  step. */
const openInstagramWebShare = async (imageUrl: string): Promise<void> => {
  if (typeof document === 'undefined') return;
  // Trigger a download. The anchor is disposable — we create, click, drop.
  const anchor = document.createElement('a');
  anchor.href = imageUrl;
  anchor.download = `mootd-${Date.now()}.png`;
  anchor.rel = 'noopener';
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();

  // Best-effort: also copy the URL so the user can paste it into Instagram
  // if they want to link rather than upload.
  try {
    if (navigator?.clipboard) {
      await navigator.clipboard.writeText(imageUrl);
    }
  } catch {
    // Clipboard access may be blocked; ignore.
  }

  if (typeof window !== 'undefined') {
    window.open('https://www.instagram.com/', '_blank', 'noopener,noreferrer');
  }
};

/** Shareable result codes callers can use to drive toasts / analytics. */
export type ShareResult =
  | { kind: 'shared' }
  | { kind: 'downloaded'; message: string }
  | { kind: 'dismissed' }
  | { kind: 'error'; error: unknown };

/** Main entry point. Call with the target platform and the absolute URL
 *  of the rendered moodboard. Never throws — errors come back as a
 *  discriminated union so the UI can decide the right message. */
export const shareMoodboard = async (
  platform: SharePlatform,
  target: ShareTarget,
): Promise<ShareResult> => {
  if (!target.imageUrl) {
    return { kind: 'error', error: new Error('missing imageUrl') };
  }

  if (Platform.OS === 'web') {
    try {
      if (platform === 'facebook') {
        openFacebookWebShare(target.imageUrl);
        return { kind: 'shared' };
      }
      await openInstagramWebShare(target.imageUrl);
      return {
        kind: 'downloaded',
        message: 'Image saved. Opening Instagram — upload it from your gallery.',
      };
    } catch (error) {
      return { kind: 'error', error };
    }
  }

  // Native: delegate to the system share sheet. The user picks IG or FB
  // from the list; we can't pre-select because only the OS knows which
  // apps are installed.
  try {
    const result = await Share.share({
      url: target.imageUrl,
      message: target.caption ?? target.imageUrl,
      title: 'Share moodboard',
    });
    if (result.action === Share.dismissedAction) {
      return { kind: 'dismissed' };
    }
    return { kind: 'shared' };
  } catch (error) {
    return { kind: 'error', error };
  }
};
