import type { ViewProps } from 'react-native';
import type { IconName } from '../../icons';

export interface AddTabProps extends Omit<ViewProps, 'children'> {
  /**
   * Label text displayed below the icon.
   * @default 'Label'
   */
  label?: string;
  /**
   * Icon name to display.
   * @default 'plus'
   */
  icon?: IconName;
  /**
   * Whether the tab is currently selected.
   * When selected, displays with a pill background.
   * @default false
   */
  selected?: boolean;
  /**
   * Whether the tab is disabled.
   */
  disabled?: boolean;
  /**
   * Callback when the tab is pressed.
   */
  onPress?: () => void;
}
