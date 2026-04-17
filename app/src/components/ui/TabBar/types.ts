import type { ViewProps } from 'react-native';
import type { IconName } from '../../icons';

export interface TabItem {
  /**
   * Unique identifier for the tab.
   */
  id: string;
  /**
   * Label text displayed below the icon.
   */
  label: string;
  /**
   * Icon name to display.
   */
  icon: IconName;
  /**
   * Whether the tab is disabled.
   */
  disabled?: boolean;
}

export interface TabBarProps extends Omit<ViewProps, 'children'> {
  /**
   * Array of tab items to display.
   */
  tabs: TabItem[];
  /**
   * ID of the currently selected tab.
   */
  selectedId?: string;
  /**
   * Callback when a tab is pressed.
   */
  onTabPress?: (tab: TabItem) => void;
}
