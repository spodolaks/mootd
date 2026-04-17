import { StyleSheet } from 'react-native';
import { labels, accents, fills, grays, ColorMode } from '../../../theme/colors';
import { spacing } from '../../../theme/spacing';
import { radius } from '../../../theme/radius';

const INPUT_HEIGHT = 54;
const MULTILINE_MIN_HEIGHT = 120;

export const getStyles = (colorScheme: ColorMode) =>
  StyleSheet.create({
    container: {
      marginBottom: spacing.md,
    },
    headerContainer: {
      marginBottom: 0,
    },
    title: {
      color: labels.primary[colorScheme],
      marginBottom: spacing.sm,
    },
    titleDisabled: {
      opacity: 0.4,
    },
    description: {
      color: labels.tertiary[colorScheme],
    },
    descriptionDisabled: {
      opacity: 0.4,
    },
    inputContainer: {
      flexDirection: 'row',
      alignItems: 'center',
      height: INPUT_HEIGHT,
      borderRadius: radius.xl,
      backgroundColor: fills.tertiary[colorScheme],
      borderWidth: 1,
      borderColor: 'transparent',
      paddingHorizontal: spacing.md,
    },
    inputContainerFocused: {
      borderColor: grays.black[colorScheme],
      borderWidth: 2,
    },
    inputContainerError: {
      borderColor: accents.red[colorScheme],
      borderWidth: 2,
    },
    inputContainerDisabled: {
      opacity: 0.4,
    },
    inputContainerMultiline: {
      height: 'auto',
      minHeight: MULTILINE_MIN_HEIGHT,
      alignItems: 'flex-start',
      paddingVertical: spacing.md,
    },
    input: {
      flex: 1,
      height: '100%',
      color: labels.primary[colorScheme],
      padding: 0,
    },
    inputWithLeftIcon: {
      marginLeft: spacing.sm,
    },
    inputWithRightIcon: {
      marginRight: spacing.sm,
    },
    inputMultiline: {
      height: 'auto',
      minHeight: MULTILINE_MIN_HEIGHT - spacing.md * 2,
    },
    inputDisabled: {
      color: labels.tertiary[colorScheme],
    },
    leftIcon: {
      justifyContent: 'center',
      alignItems: 'center',
    },
    rightIcon: {
      justifyContent: 'center',
      alignItems: 'center',
    },
    errorText: {
      marginTop: spacing.xs,
      color: accents.red[colorScheme],
    },
  });
