import type { OutfitWeather } from '@/src/domain';

export const CONDITION_ICON: Record<string, string> = {
  clear: '☀',
  sunny: '☀',
  sun: '☀',
  cloud: '☁',
  cloudy: '☁',
  overcast: '☁',
  rain: '☂',
  rainy: '☂',
  drizzle: '☂',
  shower: '☂',
  snow: '❄',
  snowy: '❄',
  sleet: '❄',
  storm: '⚡',
  thunder: '⚡',
  fog: '☷',
  foggy: '☷',
  mist: '☷',
  haze: '☷',
  wind: '≈',
  windy: '≈',
};

export const conditionIcon = (condition?: string): string => {
  if (!condition) return '';
  const key = condition.toLowerCase();
  return CONDITION_ICON[key] ?? CONDITION_ICON[key.split(' ')[0]] ?? '';
};

/**
 * Formats an `OutfitWeather` into a compact chip string, e.g. `☁ 19°C Overcast`.
 * Returns `null` when nothing usable is present so callers can conditionally
 * skip rendering without juggling empty strings.
 */
export const formatWeatherChip = (weather?: OutfitWeather): string | null => {
  if (!weather) return null;
  const icon = conditionIcon(weather.condition);
  const temp = weather.temperature
    ? `${weather.temperature}°${weather.unit ? weather.unit.toUpperCase() : ''}`
    : '';
  const label = weather.condition
    ? weather.condition.charAt(0).toUpperCase() + weather.condition.slice(1)
    : '';
  const parts = [icon, temp, label].filter(Boolean);
  return parts.length > 0 ? parts.join(' ') : null;
};
