import * as Location from 'expo-location';
import { useCallback, useEffect, useState } from 'react';
import { Platform } from 'react-native';
import type { IconName } from '@/src/components/icons/Icon';
import { usePreferencesStore } from '@/src/store/preferencesStore';

export interface WeatherData {
  temperature: number;
  unit: 'c' | 'f';
  condition: string;
  icon: IconName;
  lowTemperature: number;
  highTemperature: number;
  location: string;
}

export interface UseWeatherResult {
  weather: WeatherData | null;
  loading: boolean;
  error: string | null;
  /**
   * True when weather is unavailable specifically because the OS location
   * permission has not been granted. The hook CHECKS permission (it never
   * prompts) — requesting is the dedicated Permissions screen's job. Consumers
   * can use this to offer an "enable location" affordance instead of silently
   * showing no weather. `weather` stays null in this state (we never fabricate
   * a fallback city), so outfit generation proceeds weather-less.
   */
  needsLocationPermission: boolean;
  refresh: () => Promise<void>;
}

const CACHE_KEY = 'mootd-weather-cache';
const CACHE_TTL_MS = 30 * 60 * 1000; // 30 minutes

interface WeatherCache {
  data: WeatherData;
  cachedAt: number;
}

async function loadCached(): Promise<WeatherData | null> {
  try {
    let raw: string | null = null;
    if (Platform.OS === 'web') {
      raw = localStorage.getItem(CACHE_KEY);
    } else {
      // eslint-disable-next-line @typescript-eslint/no-require-imports
      const AsyncStorage = require('@react-native-async-storage/async-storage').default;
      raw = await AsyncStorage.getItem(CACHE_KEY);
    }
    if (!raw) return null;
    const cached = JSON.parse(raw) as WeatherCache;
    // Reject entries missing the timestamp (legacy format) or older than TTL.
    if (!cached.cachedAt || Date.now() - cached.cachedAt > CACHE_TTL_MS) {
      return null;
    }
    return cached.data;
  } catch {
    return null;
  }
}

function saveCache(data: WeatherData): void {
  const entry: WeatherCache = { data, cachedAt: Date.now() };
  const json = JSON.stringify(entry);
  if (Platform.OS === 'web') {
    try {
      localStorage.setItem(CACHE_KEY, json);
    } catch {
      /* ignore */
    }
  } else {
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const AsyncStorage = require('@react-native-async-storage/async-storage').default;
    void (async () => {
      try {
        await AsyncStorage.setItem(CACHE_KEY, json);
      } catch {
        /* ignore */
      }
    })();
  }
}

function interpretWeatherCode(code: number): { condition: string; icon: IconName } {
  if (code === 0) return { condition: 'Clear', icon: 'sun' };
  if (code <= 2) return { condition: 'Partly Cloudy', icon: 'cloud' };
  if (code === 3) return { condition: 'Overcast', icon: 'cloud' };
  if (code <= 48) return { condition: 'Foggy', icon: 'cloud' };
  if (code <= 55) return { condition: 'Drizzle', icon: 'umbrella' };
  if (code <= 65) return { condition: 'Rainy', icon: 'umbrella' };
  if (code <= 77) return { condition: 'Snowy', icon: 'cloud' };
  if (code <= 82) return { condition: 'Showers', icon: 'umbrella' };
  if (code <= 86) return { condition: 'Snow Showers', icon: 'cloud' };
  return { condition: 'Thunderstorm', icon: 'umbrella' };
}

export function useWeather(): UseWeatherResult {
  const temperatureUnit = usePreferencesStore(s => s.temperatureUnit);
  const [weather, setWeather] = useState<WeatherData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [needsLocationPermission, setNeedsLocationPermission] = useState(false);

  // Hydrate from cache on first mount so the card is visible immediately.
  useEffect(() => {
    loadCached().then(cached => {
      if (cached) {
        console.log(
          '[Weather] Loaded from cache:',
          cached.location,
          cached.temperature + '°' + cached.unit
        );
        setWeather(cached);
      }
    });
  }, []);

  const fetchWeather = useCallback(async () => {
    setLoading(true);
    setError(null);
    setNeedsLocationPermission(false);
    console.log('[Weather] → Checking location permission...');
    try {
      let latitude: number;
      let longitude: number;

      if (Platform.OS === 'web') {
        // On web, expo-location's permission API isn't meaningful — the
        // browser's geolocation call handles its own permission prompt and
        // rejects if the user denies. We never call this on the hook's behalf
        // beyond reading the position, so let it run and surface any denial
        // as a clean "unavailable" state below (no fabricated fallback city).
        const coords = await new Promise<{ latitude: number; longitude: number }>(
          (resolve, reject) => {
            if (!navigator.geolocation) {
              reject(new Error('Geolocation not supported'));
              return;
            }
            navigator.geolocation.getCurrentPosition(
              pos => resolve({ latitude: pos.coords.latitude, longitude: pos.coords.longitude }),
              err => reject(new Error(err.message)),
              { enableHighAccuracy: false, timeout: 10000 }
            );
          }
        );
        latitude = coords.latitude;
        longitude = coords.longitude;
      } else {
        // CHECK (don't request) the OS permission. Requesting belongs to the
        // dedicated Permissions screen — popping the system prompt from a
        // screen mount gives the user zero context. If permission isn't
        // granted, surface a needs-permission state and leave weather null
        // instead of fabricating a wrong-city (Tallinn) fallback.
        const { status } = await Location.getForegroundPermissionsAsync();
        if (status !== 'granted') {
          console.log('[Weather] ⚠ Location permission not granted — weather unavailable');
          setNeedsLocationPermission(true);
          setLoading(false);
          return;
        }

        // Permission granted: try last-known first (instant), then a fresh fix.
        const last = await Location.getLastKnownPositionAsync();
        const coords = last
          ? { latitude: last.coords.latitude, longitude: last.coords.longitude }
          : await Location.getCurrentPositionAsync({
              accuracy: Location.Accuracy.Balanced,
            }).then(p => ({ latitude: p.coords.latitude, longitude: p.coords.longitude }));
        latitude = coords.latitude;
        longitude = coords.longitude;
      }

      console.log(
        `[Weather] ✓ Location acquired: ${latitude.toFixed(4)}, ${longitude.toFixed(4)}`
      );

      const apiUnit = temperatureUnit === 'fahrenheit' ? 'fahrenheit' : 'celsius';
      const displayUnit: 'c' | 'f' = temperatureUnit === 'fahrenheit' ? 'f' : 'c';

      console.log('[Weather] → Fetching weather + geocode...');
      const [weatherRes, geoRes] = await Promise.all([
        fetch(
          `https://api.open-meteo.com/v1/forecast?latitude=${latitude}&longitude=${longitude}&current=temperature_2m,weathercode&daily=temperature_2m_max,temperature_2m_min&timezone=auto&temperature_unit=${apiUnit}&forecast_days=1`
        ),
        fetch(
          `https://nominatim.openstreetmap.org/reverse?lat=${latitude}&lon=${longitude}&format=json`
        ),
      ]);

      if (!weatherRes.ok) throw new Error('Weather service unavailable');
      const weatherJson = await weatherRes.json();

      const { condition, icon } = interpretWeatherCode(weatherJson.current.weathercode as number);

      let locationName = 'Current Location';
      if (geoRes.ok) {
        const geoJson = await geoRes.json();
        const addr = geoJson.address ?? {};
        const city =
          (addr.city as string | undefined) ??
          (addr.town as string | undefined) ??
          (addr.village as string | undefined) ??
          (addr.county as string | undefined) ??
          (addr.state as string | undefined) ??
          'Unknown';
        const countryCode = (addr.country_code as string | undefined)?.toUpperCase() ?? '';
        locationName = countryCode ? `${city}, ${countryCode}` : city;
      }

      const fresh: WeatherData = {
        temperature: Math.round(weatherJson.current.temperature_2m as number),
        unit: displayUnit,
        condition,
        icon,
        lowTemperature: Math.round((weatherJson.daily.temperature_2m_min as number[])[0]),
        highTemperature: Math.round((weatherJson.daily.temperature_2m_max as number[])[0]),
        location: locationName,
      };

      console.log(
        `[Weather] ✓ ${fresh.condition}, ${fresh.temperature}°${fresh.unit}, ${fresh.location}`
      );
      setWeather(fresh);
      saveCache(fresh);
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to load weather';
      console.log('[Weather] ✗ Error:', msg);
      setError(msg);
    } finally {
      setLoading(false);
    }
  }, [temperatureUnit]);

  useEffect(() => {
    void fetchWeather();
  }, [fetchWeather]);

  return { weather, loading, error, needsLocationPermission, refresh: fetchWeather };
}
