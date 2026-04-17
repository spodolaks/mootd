import { StyleSheet, TextStyle, ViewStyle } from 'react-native';
import { ColorMode, backgrounds, separators, labels } from '../../../theme/colors';
import { radius } from '../../../theme/radius';
import { spacing } from '../../../theme/spacing';
import { typography } from '../../../theme/typography';

interface InfoStyles {
  container: ViewStyle;
  contentContainer: ViewStyle;
  iconContainer: ViewStyle;
  textContainer: ViewStyle;
  title: TextStyle;
  description: TextStyle;
  closeButton: ViewStyle;
}

export function getStyles(colorScheme: ColorMode): InfoStyles {
  return StyleSheet.create<InfoStyles>({
    container: {
      flexDirection: 'row',
      alignItems: 'center',
      backgroundColor: backgrounds.secondary[colorScheme],
      borderRadius: radius.xl,
      paddingHorizontal: 19,
      paddingVertical: spacing.md,
      minHeight: 79,
      borderWidth: 1,
      borderColor: separators.secondary[colorScheme],
    },
    contentContainer: {
      flexDirection: 'row',
      alignItems: 'center',
      flex: 1,
    },
    iconContainer: {
      marginRight: 18,
    },
    textContainer: {
      flex: 1,
    },
    title: {
      ...typography.callout.semiBold,
      color: labels.primary[colorScheme],
    },
    description: {
      ...typography.body.regular,
      color: labels.tertiary[colorScheme],
      marginTop: spacing.xs,
    },
    closeButton: {
      padding: spacing.xs,
      marginLeft: spacing.sm,
    },
  });
}
