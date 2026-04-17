import type { ViewStyle } from 'react-native';
import type { ListItemProps } from '../ListItem/types';

export interface ListProps {
  /**
   * Array of list item configurations.
   */
  items: Omit<ListItemProps, 'position'>[];
  /**
   * Optional header text displayed above the list.
   */
  header?: string;
  /**
   * Optional footer text displayed below the list.
   */
  footer?: string;
  /**
   * Custom style for the list container.
   */
  style?: ViewStyle;
  /**
   * Custom style for the items container.
   */
  itemsContainerStyle?: ViewStyle;
}
