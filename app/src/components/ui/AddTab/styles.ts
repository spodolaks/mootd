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
      paddingHorizontal: 16,
      paddingVertical: 10,
      minWidth: 75,
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
