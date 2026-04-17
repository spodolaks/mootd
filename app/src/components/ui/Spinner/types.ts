import type { ViewStyle } from 'react-native';

export interface SpinnerProps {
  /**
   * Size multiplier for the spinner.
   * @default 1
   */
  size?: number;
  /**
   * Custom style for the container.
   */
  style?: ViewStyle;
  /**
   * Whether to animate the spinner arcs.
   * @default true
   */
  animated?: boolean;
}
