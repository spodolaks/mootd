import { StyleSheet } from 'react-native';
import { spacing } from '../../../theme/spacing';

export const getStyles = () => {
  return StyleSheet.create({
    container: {
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'center',
    },
    dot: {
      width: spacing.sm,
      height: spacing.sm,
      borderRadius: spacing.sm / 2,
    },
  });
};
