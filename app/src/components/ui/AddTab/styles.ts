import { StyleSheet } from 'react-native';
import { labels } from '../../../theme/colors';

export const getStyles = (colorScheme: 'light' | 'dark', selected: boolean = false) => {
  const selectedBg =
    colorScheme === 'light' ? 'rgba(120, 120, 128, 0.16)' : 'rgba(120, 120, 128, 0.32)';

  return StyleSheet.create({
    container: {
      flexDirection: 'column',
      alignItems: 'center',
      justifyContent: 'center',
      backgroundColor: selected ? selectedBg : 'transparent',
      borderRadius: 999,
      // Horizontal padding tightened from 16 → 10 so "Moodboard" (the longest
      // label) fits on one line inside the tab's share of the pill width;
      // otherwise the word wraps and the selected-state bg grows taller than
      // the other tabs, making the active tab look oversized.
      paddingHorizontal: 10,
      paddingVertical: 10,
      minWidth: 64,
      minHeight: 54,
      gap: 4,
    },
    label: {
      fontSize: 11,
      fontWeight: '400',
      color: labels.primary[colorScheme],
      textAlign: 'center',
    },
  });
};
