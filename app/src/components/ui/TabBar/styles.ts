import { StyleSheet } from 'react-native';
import { separators, backgrounds } from '../../../theme/colors';
import { spacing } from '../../../theme/spacing';

export const getStyles = (colorScheme: 'light' | 'dark') =>
  StyleSheet.create({
    container: {
      backgroundColor: backgrounds.secondary[colorScheme],
      borderTopWidth: 1,
      borderTopColor: separators.primary[colorScheme],
      paddingHorizontal: spacing.md,
      paddingTop: spacing.sm,
      paddingBottom: spacing.lg,
    },
    tabsRow: {
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'flex-start',
      gap: spacing.sm,
    },
    tab: {
      flex: 1,
    },
  });
