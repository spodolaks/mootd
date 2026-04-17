import { ViewStyle } from 'react-native';

export interface SegmentedProgressBarProps {
  /**
   * Total number of segments
   */
  totalSegments: number;
  /**
   * Current active segment (0-indexed)
   */
  currentSegment: number;
  /**
   * Additional styles for the container
   */
  style?: ViewStyle;
  /**
   * Show a fade gradient below the progress bar (useful when content scrolls underneath)
   */
  withFade?: boolean;
}
