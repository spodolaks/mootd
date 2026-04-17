import { StyleSheet } from 'react-native';
import { ColorMode, labels, fills, backgrounds } from '../../../theme/colors';
import { typography } from '../../../theme/typography';
import { spacing } from '../../../theme/spacing';

export const getStyles = (colorScheme: ColorMode) =>
  StyleSheet.create({
    container: {
      borderRadius: 24,
      backgroundColor: backgrounds.secondary[colorScheme],
      paddingHorizontal: 20,
      paddingVertical: 14,
      flexDirection: 'row',
    },

    // Left section: weather info
    leftSection: {
      flex: 1,
    },

    // Top row: icon and temperature
    topRow: {
      flexDirection: 'row',
      alignItems: 'center',
    },

    // Weather icon container
    iconContainer: {
      width: 32,
      height: 32,
      justifyContent: 'center',
      alignItems: 'center',
      marginRight: spacing.sm,
    },

    // Temperature with degree symbol and unit
    temperatureRow: {
      flexDirection: 'row',
      alignItems: 'flex-start',
    },

    temperature: {
      ...typography.title1.regular,
      color: labels.primary[colorScheme],
    },

    degreeUnitColumn: {
      alignItems: 'center',
      marginTop: spacing.xs,
    },

    degreeSymbol: {
      ...typography.title1.regular,
      color: labels.primary[colorScheme],
      lineHeight: 24,
    },

    unitText: {
      ...typography.caption1.regular,
      color: labels.primary[colorScheme],
      marginTop: -12,
    },

    // Bottom row: condition and high/low
    bottomRow: {
      flexDirection: 'row',
      alignItems: 'center',
      marginTop: spacing.xs,
      gap: spacing.md,
    },

    condition: {
      ...typography.callout.semiBold,
      color: labels.primary[colorScheme],
    },

    highLowText: {
      ...typography.callout.regular,
      color: labels.secondary[colorScheme],
    },

    // Right section: location with dropdown (centered vertically)
    rightSection: {
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'center',
      gap: spacing.sm,
    },

    location: {
      ...typography.callout.regular,
      color: labels.primary[colorScheme],
    },

    dropdownButton: {
      width: 28,
      height: 28,
      borderRadius: 14,
      backgroundColor: fills.tertiary[colorScheme],
      justifyContent: 'center',
      alignItems: 'center',
    },
  });
