import { StyleSheet } from 'react-native';
import { labels } from '../../../theme/colors';

export const getStyles = (colorScheme: 'light' | 'dark') => {
  return StyleSheet.create({
    base: {
      color: labels.primary[colorScheme],
    },
  });
};
