/**
 * Color scheme hook + provider.
 *
 * Reads the user's theme preference from the unified preferencesStore
 * (Zustand with persistence) so that changing the preference in the
 * PreferencesScreen immediately updates every screen that calls
 * useColorScheme().
 */

import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react';
import { Appearance } from 'react-native';
import { usePreferencesStore } from '@/src/store/preferencesStore';

export type ColorScheme = 'light' | 'dark';
export type ThemePreference = 'light' | 'dark' | 'system';

interface ColorSchemeContextType {
  colorScheme: ColorScheme;
  themePreference: ThemePreference;
  setThemePreference: (preference: ThemePreference) => void;
}

const ColorSchemeContext = createContext<ColorSchemeContextType | undefined>(undefined);

interface ColorSchemeProviderProps {
  children: ReactNode;
}

const resolveSystemScheme = (): ColorScheme =>
  Appearance.getColorScheme() === 'dark' ? 'dark' : 'light';

export const ColorSchemeProvider: React.FC<ColorSchemeProviderProps> = ({ children }) => {
  // Read theme preference from the unified preferences store
  const themePreference = usePreferencesStore((s) => s.theme);
  const setTheme = usePreferencesStore((s) => s.setTheme);

  const [systemScheme, setSystemScheme] = useState<ColorScheme>(resolveSystemScheme);

  const colorScheme: ColorScheme =
    themePreference === 'system' ? systemScheme : themePreference;

  useEffect(() => {
    const subscription = Appearance.addChangeListener(({ colorScheme: newScheme }) => {
      setSystemScheme(newScheme === 'dark' ? 'dark' : 'light');
    });
    return () => subscription.remove();
  }, []);

  return (
    <ColorSchemeContext.Provider
      value={{
        colorScheme,
        themePreference,
        setThemePreference: setTheme,
      }}
    >
      {children}
    </ColorSchemeContext.Provider>
  );
};

export const useColorScheme = (): ColorScheme => {
  const context = useContext(ColorSchemeContext);

  if (context === undefined) {
    return resolveSystemScheme();
  }

  return context.colorScheme;
};

export const useThemePreference = () => {
  const context = useContext(ColorSchemeContext);

  if (context === undefined) {
    throw new Error('useThemePreference must be used within a ColorSchemeProvider');
  }

  return {
    themePreference: context.themePreference,
    setThemePreference: context.setThemePreference,
  };
};
