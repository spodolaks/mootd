import { StyleSheet } from 'react-native';
import { typography } from '../../../theme/typography';
import { radius, spacing } from '../../../theme';

export const getStyles = () => {
  return StyleSheet.create({
    container: {
      flexDirection: 'row',
      alignItems: 'center',
      borderRadius: radius.xl,
      minHeight: 56,
    },
    iconContainer: {
      marginRight: spacing.sm + 4,
    },
    label: {
      ...typography.body.regular,
    },
    disabled: {
      opacity: 0.5,
    },
  });
};
