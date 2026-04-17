/**
 * Hook to get theme-aware colors from the design system
 */

import { colors, ColorMode } from '@/src/theme';
import { useColorScheme } from './useColorScheme';

type ThemeColorProps = { light?: string; dark?: string };

/**
 * Returns a color based on the current color scheme.
 * If light/dark props are provided, uses those. Otherwise falls back to theme defaults.
 */
export function useThemeColor(
  props: ThemeColorProps,
  fallback?: { light: string; dark: string }
): string {
  const colorScheme = useColorScheme();
  const colorFromProps = props[colorScheme];

  if (colorFromProps) {
    return colorFromProps;
  }

  if (fallback) {
    return fallback[colorScheme];
  }

  // Default fallback to label primary
  return colors.labels.primary[colorScheme];
}

/**
 * Returns the current color mode
 */
export function useColorMode(): ColorMode {
  return useColorScheme();
}
