import type { ViewStyle } from 'react-native';

export interface SegmentedControlOption {
  /**
   * The label to display for this segment.
   */
  label: string;
  /**
   * Optional value associated with this segment. Defaults to the label.
   */
  value?: string;
}

export interface SegmentedControlProps {
  /**
   * Array of options to display as segments.
   */
  options: SegmentedControlOption[] | string[];
  /**
   * The currently selected value (or index if no values provided).
   */
  selectedValue: string;
  /**
   * Callback when a segment is selected.
   */
  onValueChange: (value: string) => void;
  /**
   * Whether the control is disabled.
   * @default false
   */
  disabled?: boolean;
  /**
   * Custom style for the container.
   */
  style?: ViewStyle;
}
