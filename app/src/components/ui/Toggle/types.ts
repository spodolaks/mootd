import type { ViewStyle } from 'react-native';

export interface ToggleProps {
  /**
   * Whether the toggle is on or off.
   */
  value: boolean;
  /**
   * Callback when the toggle value changes.
   */
  onValueChange: (value: boolean) => void;
  /**
   * Whether the toggle is disabled.
   * @default false
   */
  disabled?: boolean;
  /**
   * Custom style for the toggle container.
   */
  style?: ViewStyle;
}
