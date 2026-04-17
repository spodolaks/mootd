import { StyleSheet } from 'react-native';
import type { GradientIconButtonSize } from './types';

/**
 * Size configurations matching Button's icon-only sizes:
 * - xs: 28x28, radius 14
 * - sm: 34x34, radius 17
 * - md: 50x50, radius 25
 * - lg: 50x50, radius 25
 */
export const SIZES: Record<GradientIconButtonSize, { dimension: number; radius: number }> = {
  xs: { dimension: 28, radius: 14 },
  sm: { dimension: 34, radius: 17 },
  md: { dimension: 50, radius: 25 },
  lg: { dimension: 50, radius: 25 },
};

export const getStyles = () =>
  StyleSheet.create({
    wrapper: {
      position: 'relative',
    },
    touchable: {
      position: 'relative',
    },
    content: {
      position: 'absolute',
      top: 0,
      left: 0,
      right: 0,
      bottom: 0,
      alignItems: 'center',
      justifyContent: 'center',
    },
    disabled: {
      opacity: 0.5,
    },
  });
