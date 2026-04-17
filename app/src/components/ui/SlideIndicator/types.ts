import type { ViewStyle } from 'react-native';

export interface SlideIndicatorProps {
  /**
   * Total number of slides/dots to display.
   */
  totalDots: number;
  /**
   * Index of the currently active dot (0-based).
   */
  activeIndex: number;
  /**
   * Custom style for the container.
   */
  style?: ViewStyle;
}
