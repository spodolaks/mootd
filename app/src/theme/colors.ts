// Colors extracted from Figma design system

// Accent colors with light/dark mode support
export const accents = {
  brand: {
    light: '#A09E95',
    dark: '#FFFFFF',
  },
  red: {
    light: '#FF383C',
    dark: '#FF4245',
  },
  orange: {
    light: '#FF8D28',
    dark: '#FF9230',
  },
  yellow: {
    light: '#FFCC00',
    dark: '#FFD600',
  },
  green: {
    light: '#34C759',
    dark: '#30D158',
  },
  mint: {
    light: '#00C8B3',
    dark: '#00DAC3',
  },
  teal: {
    light: '#00C3D0',
    dark: '#00D2E0',
  },
  cyan: {
    light: '#00C0E8',
    dark: '#3CD3FE',
  },
  blue: {
    light: '#0088FF',
    dark: '#0091FF',
  },
  indigo: {
    light: '#6B5DFF',
    dark: '#6D7CFF',
  },
  purple: {
    light: '#CB30E0',
    dark: '#DB34F2',
  },
  pink: {
    light: '#FF2D55',
    dark: '#FF375F',
  },
  brown: {
    light: '#AC7F5E',
    dark: '#B78A66',
  },
} as const;

// Gray scale
export const grays = {
  black: {
    light: '#000000',
    dark: '#FFFFFF',
  },
  gray: {
    light: '#8E8E93',
    dark: '#8E8E93',
  },
  gray2: {
    light: '#AEAEB2',
    dark: '#636366',
  },
  gray3: {
    light: '#C7C7CC',
    dark: '#48484A',
  },
  gray4: {
    light: '#D1D1D6',
    dark: '#3A3A3C',
  },
  gray5: {
    light: '#E5E5EA',
    dark: '#2C2C2E',
  },
  gray6: {
    light: '#F2F2F7',
    dark: '#1C1C1E',
  },
  white: {
    light: '#FFFFFF',
    dark: '#000000',
  },
} as const;

// Background colors
export const backgrounds = {
  primary: {
    light: '#F2F2F7',
    dark: '#000000',
  },
  secondary: {
    light: '#FFFFFF',
    dark: '#1C1C1E',
  },
  tertiary: {
    light: '#F2F2F7',
    dark: '#2C2C2E',
  },
} as const;

// Fill colors (with transparency)
export const fills = {
  primary: {
    light: 'rgba(120, 120, 120, 0.2)',
    dark: 'rgba(120, 120, 128, 0.36)',
  },
  secondary: {
    light: 'rgba(120, 120, 128, 0.16)',
    dark: 'rgba(120, 120, 128, 0.32)',
  },
  tertiary: {
    light: 'rgba(118, 118, 128, 0.12)',
    dark: 'rgba(118, 118, 128, 0.24)',
  },
  quaternary: {
    light: 'rgba(116, 116, 128, 0.08)',
    dark: 'rgba(118, 118, 128, 0.18)',
  },
} as const;

// Vibrant fill colors
export const fillsVibrant = {
  primary: {
    light: '#CCCCCC',
    dark: '#333333',
  },
  secondary: {
    light: '#E0E0E0',
    dark: '#1F1F1F',
  },
  tertiary: {
    light: '#EDEDED',
    dark: '#121212',
  },
} as const;

// Label colors (text)
export const labels = {
  primary: {
    light: '#000000',
    dark: '#FFFFFF',
  },
  secondary: {
    light: 'rgba(60, 60, 67, 1)',
    dark: 'rgba(235, 235, 245, 1)',
  },
  tertiary: {
    light: 'rgba(60, 60, 67, 0.6)',
    dark: 'rgba(235, 235, 245, 0.6)',
  },
  quaternary: {
    light: 'rgba(60, 60, 67, 0.3)',
    dark: 'rgba(235, 235, 245, 0.3)',
  },
} as const;

// Separator colors
export const separators = {
  primary: {
    light: 'rgba(0, 0, 0, 0.12)',
    dark: 'rgba(255, 255, 255, 0.12)',
  },
  secondary: {
    light: 'rgba(0, 0, 0, 0.06)',
    dark: 'rgba(255, 255, 255, 0.06)',
  },
  tertiary: {
    light: '#E6E6E6',
    dark: '#1A1A1A',
  },
} as const;

// Overlay colors
export const overlays = {
  default: {
    light: 'rgba(0, 0, 0, 0.4)',
    dark: 'rgba(0, 0, 0, 0.7)',
  },
} as const;

// Gradient colors for buttons and decorative elements
export const gradients = {
  // Primary colorful gradient: yellow -> orange -> purple -> indigo
  primary: {
    colors: ['#FFCC00', '#FF8D28', '#CB30E0', '#6B5DFF'] as const,
    locations: [0, 0.23, 0.79, 1] as const,
  },
} as const;

// Button colors
export const button = {
  primary: {
    background: {
      light: '#000000',
      dark: '#FFFFFF',
    },
    foreground: {
      light: '#FFFFFF',
      dark: '#000000',
    },
    disabledOpacity: 0.3,
  },
  secondary: {
    background: {
      light: 'rgba(118, 118, 128, 0.12)',
      dark: 'rgba(118, 118, 128, 0.24)',
    },
    foreground: {
      light: '#000000',
      dark: '#FFFFFF',
    },
    disabledOpacity: 0.4,
  },
  ghost: {
    background: {
      light: 'transparent',
      dark: 'transparent',
    },
    foreground: {
      light: '#000000',
      dark: '#FFFFFF',
    },
    disabledOpacity: 0.4,
  },
} as const;

// Combined colors export for easy access
export const colors = {
  accents,
  grays,
  backgrounds,
  fills,
  fillsVibrant,
  labels,
  separators,
  overlays,
  gradients,
  button,
  // Legacy/convenience aliases
  transparent: 'transparent',
} as const;

export type Colors = typeof colors;
export type ColorMode = 'light' | 'dark';

// Helper function to get color based on mode
export function getColor<T extends { light: string; dark: string }>(
  color: T,
  mode: ColorMode
): string {
  return color[mode];
}
