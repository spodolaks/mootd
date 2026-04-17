import type { ViewProps } from 'react-native';

export type SelectableItemVariant = 'simple' | 'withIcon';

export interface SelectableItemProps extends Omit<ViewProps, 'children'> {
  /**
   * The label text to display
   */
  label: string;
  /**
   * Whether the item is selected
   * @default false
   */
  selected?: boolean;
  /**
   * The variant of the selectable item
   * @default 'simple'
   */
  variant?: SelectableItemVariant;
  /**
   * Callback when the item is pressed
   */
  onPress?: () => void;
  /**
   * Whether the item is disabled.
   */
  disabled?: boolean;
}
