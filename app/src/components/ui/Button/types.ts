import type { ViewProps } from 'react-native';
import type { IconName } from '../../icons';

export type ButtonVariant = 'primary' | 'secondary' | 'ghost';
export type ButtonSize = 'xs' | 'sm' | 'md' | 'lg';

export interface ButtonProps extends Omit<ViewProps, 'children'> {
  /**
   * Button label text. If not provided, button will be icon-only.
   */
  label?: string;
  /**
   * Icon name to display. Required for icon-only buttons.
   */
  icon?: IconName;
  /**
   * Position of the icon relative to the label.
   * @default 'left'
   */
  iconPosition?: 'left' | 'right';
  /**
   * Button variant.
   * @default 'primary'
   */
  variant?: ButtonVariant;
  /**
   * Button size.
   * @default 'md'
   */
  size?: ButtonSize;
  /**
   * Whether the button is disabled.
   */
  disabled?: boolean;
  /**
   * Callback when the button is pressed.
   */
  onPress?: () => void;
}
