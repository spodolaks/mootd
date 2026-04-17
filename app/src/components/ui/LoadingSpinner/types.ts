import type { ViewStyle } from 'react-native';

export interface LoadingSpinnerProps {
  /**
   * Size of the spinner in pixels (diameter).
   * @default 28
   */
  size?: number;
  /**
   * Custom style for the container.
   */
  style?: ViewStyle;
  /**
   * Custom color for the spinner arc.
   * If not provided, uses labels.tertiary from theme.
   */
  color?: string;
}
