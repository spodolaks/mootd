import { StyleSheet, TextStyle, ViewStyle } from 'react-native';
import { ColorMode, backgrounds, labels, separators } from '../../../theme/colors';
import { typography } from '../../../theme/typography';
import { spacing, radius } from '../../../theme';

interface ToastStyles {
  container: ViewStyle;
  contentContainer: ViewStyle;
  iconContainer: ViewStyle;
  title: TextStyle;
}

export function getStyles(colorScheme: ColorMode): ToastStyles {
  return StyleSheet.create<ToastStyles>({
    container: {
      flexDirection: 'row',
      alignItems: 'center',
      backgroundColor: backgrounds.secondary[colorScheme],
      borderRadius: radius.xl,
      paddingHorizontal: spacing.md,
      height: 56,
      borderWidth: 1,
      borderColor: separators.secondary[colorScheme],
    },
    contentContainer: {
      flexDirection: 'row',
      alignItems: 'center',
      flex: 1,
    },
    iconContainer: {
      marginRight: spacing.sm,
    },
    title: {
      ...typography.body.regular,
      color: labels.primary[colorScheme],
      flex: 1,
    },
  });
}
