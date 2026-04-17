import type { ViewStyle } from 'react-native';
import type { IconName } from '../../icons';

export type ListItemPosition = 'first' | 'middle' | 'last' | 'single';

export interface ListItemProps {
  /**
   * Label text to display.
   */
  label: string;
  /**
   * Icon to display on the left side.
   */
  icon?: IconName;
  /**
   * Whether to show a toggle on the right side.
   */
  showToggle?: boolean;
  /**
   * Toggle value (required if showToggle is true).
   */
  toggleValue?: boolean;
  /**
   * Callback when toggle value changes.
   */
  onToggleChange?: (value: boolean) => void;
  /**
   * Callback when the item is pressed (if not using toggle).
   */
  onPress?: () => void;
  /**
   * Position in the list for proper border radius.
   * @default 'middle'
   */
  position?: ListItemPosition;
  /**
   * Whether to show the separator line.
   * @default true
   */
  showSeparator?: boolean;
  /**
   * Whether the item is disabled.
   * @default false
   */
  disabled?: boolean;
  /**
   * Custom style for the item container.
   */
  style?: ViewStyle;
}
