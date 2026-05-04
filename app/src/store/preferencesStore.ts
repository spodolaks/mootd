/**
 * Unified preferences store.
 *
 * Follows the 2025/2026 best-practice of a single, focused Zustand store per
 * domain concern with lightweight manual persistence (the zustand/middleware
 * `persist` helper relies on import.meta.env which Metro's web bundler does
 * not support).
 *
 * Theme preference lives here now instead of a separate themeStore so that all
 * user-facing settings share one persistence layer.
 */

import { create } from 'zustand';
import { Platform } from 'react-native';

// ─── Types ───────────────────────────────────────────────────────────────────

export type ThemePreference = 'light' | 'dark' | 'system';
export type TemperatureUnit = 'celsius' | 'fahrenheit';

export interface PreferencesState {
  // Appearance
  theme: ThemePreference;

  // Notifications
  pushNotifications: boolean;
  dailyOutfitReminder: boolean;
  weatherAlerts: boolean;

  // Units
  temperatureUnit: TemperatureUnit;

  // Outfit creativity (mootd#67). 0 = predictable, 1 = surprising.
  // Backend translates to LLM temperature. Cached locally so the
  // UI reads instantly + persists across cold starts; server is
  // updated optimistically on change.
  creativity: number;

  // Account (mirrors backend, cached locally)
  displayName: string;
  email: string;
}

interface PreferencesActions {
  setTheme: (theme: ThemePreference) => void;
  setPushNotifications: (enabled: boolean) => void;
  setDailyOutfitReminder: (enabled: boolean) => void;
  setWeatherAlerts: (enabled: boolean) => void;
  setTemperatureUnit: (unit: TemperatureUnit) => void;
  setCreativity: (creativity: number) => void;
  setDisplayName: (name: string) => void;
  setEmail: (email: string) => void;
  /** Bulk-hydrate from persisted storage (called once at startup). */
  hydrate: () => Promise<void>;
  /** Reset everything to defaults (used by "delete account"). */
  reset: () => void;
}

type PreferencesStore = PreferencesState & PreferencesActions;

// ─── Defaults ────────────────────────────────────────────────────────────────

const DEFAULTS: PreferencesState = {
  theme: 'system',
  pushNotifications: true,
  dailyOutfitReminder: true,
  weatherAlerts: false,
  temperatureUnit: 'celsius',
  creativity: 0.5, // mootd#67 — middle of the slider, equivalent to historical default
  displayName: '',
  email: '',
};

// ─── Persistence helpers ─────────────────────────────────────────────────────

const STORAGE_KEY = 'mootd-preferences';

function loadSync(): Partial<PreferencesState> {
  try {
    if (Platform.OS === 'web') {
      const raw = localStorage.getItem(STORAGE_KEY);
      if (raw) return JSON.parse(raw) as Partial<PreferencesState>;
    }
  } catch {
    /* ignore */
  }
  return {};
}

async function loadAsync(): Promise<Partial<PreferencesState>> {
  try {
    if (Platform.OS !== 'web') {
      // eslint-disable-next-line @typescript-eslint/no-require-imports
      const AsyncStorage = require('@react-native-async-storage/async-storage').default;
      const raw = await AsyncStorage.getItem(STORAGE_KEY);
      if (raw) return JSON.parse(raw) as Partial<PreferencesState>;
    }
  } catch {
    /* ignore */
  }
  return {};
}

function persist(state: PreferencesState): void {
  const json = JSON.stringify(state);
  if (Platform.OS === 'web') {
    try {
      localStorage.setItem(STORAGE_KEY, json);
    } catch {
      /* ignore */
    }
  } else {
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const AsyncStorage = require('@react-native-async-storage/async-storage').default;
    void (async () => {
      try {
        await AsyncStorage.setItem(STORAGE_KEY, json);
      } catch {
        /* ignore */
      }
    })();
  }
}

// ─── Helper: extract only serialisable state (no action fns) ─────────────────

function getState(store: PreferencesStore): PreferencesState {
  return {
    theme: store.theme,
    pushNotifications: store.pushNotifications,
    dailyOutfitReminder: store.dailyOutfitReminder,
    weatherAlerts: store.weatherAlerts,
    temperatureUnit: store.temperatureUnit,
    creativity: store.creativity,
    displayName: store.displayName,
    email: store.email,
  };
}

// ─── Helper: update + persist in one call ────────────────────────────────────

function update(
  set: (fn: (s: PreferencesStore) => Partial<PreferencesStore>) => void,
  get: () => PreferencesStore,
  patch: Partial<PreferencesState>,
) {
  set(() => patch);
  persist(getState(get()));
}

// ─── Store ───────────────────────────────────────────────────────────────────

const initialState: PreferencesState = { ...DEFAULTS, ...loadSync() };

export const usePreferencesStore = create<PreferencesStore>((set, get) => ({
  ...initialState,

  setTheme: (theme) => update(set, get, { theme }),
  setPushNotifications: (pushNotifications) => update(set, get, { pushNotifications }),
  setDailyOutfitReminder: (dailyOutfitReminder) => update(set, get, { dailyOutfitReminder }),
  setWeatherAlerts: (weatherAlerts) => update(set, get, { weatherAlerts }),
  setTemperatureUnit: (temperatureUnit) => update(set, get, { temperatureUnit }),
  setCreativity: (creativity) => update(set, get, { creativity }),
  setDisplayName: (displayName) => update(set, get, { displayName }),
  setEmail: (email) => update(set, get, { email }),

  hydrate: async () => {
    const persisted = await loadAsync();
    if (Object.keys(persisted).length > 0) {
      set(() => persisted);
    }
  },

  reset: () => {
    set(() => ({ ...DEFAULTS }));
    persist(DEFAULTS);
  },
}));
