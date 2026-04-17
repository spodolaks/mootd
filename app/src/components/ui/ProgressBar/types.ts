import type { ViewStyle } from 'react-native';

export interface ProgressBarProps {
  /**
   * Progress value between 0 and 1.
   * @default 0
   */
  progress: number;
  /**
   * Height of the progress bar.
   * @default 6
   */
  height?: number;
  /**
   * Custom style for the container.
   */
  style?: ViewStyle;
}
