import type { ViewProps } from 'react-native';
import type { IconName } from '../../icons';

export type GradientIconButtonSize = 'xs' | 'sm' | 'md' | 'lg';

export interface GradientIconButtonProps extends Omit<ViewProps, 'children'> {
  /**
   * Icon name to display.
   */
  icon: IconName;
  /**
   * Button size.
   * - xs: 28x28
   * - sm: 34x34
   * - md: 50x50
   * - lg: 50x50
   * @default 'md'
   */
  size?: GradientIconButtonSize;
  /**
   * Gradient colors for the border (applied around the circle).
   * Defaults to the primary gradient from design tokens.
   */
  borderGradientColors?: string[];
  /**
   * Whether the button is disabled.
   */
  disabled?: boolean;
  /**
   * Callback when the button is pressed.
   */
  onPress?: () => void;
}
