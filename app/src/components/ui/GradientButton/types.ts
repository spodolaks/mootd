import type { ViewProps } from 'react-native';
import type { IconName } from '../../icons';

export type GradientButtonVariant = 'solid' | 'gradient';

export interface GradientButtonProps extends Omit<ViewProps, 'children'> {
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
   * Button variant:
   * - 'solid': Gradient border with solid background (theme-aware)
   * - 'gradient': Full colorful gradient background with white content
   * @default 'solid'
   */
  variant?: GradientButtonVariant;
  /** Gradient colors for the border (left to right) - used in 'solid' variant */
  borderGradientColors?: string[];
  /** Gradient colors for the button background (diagonal) */
  backgroundGradientColors?: string[];
  /**
   * Whether the button is disabled.
   */
  disabled?: boolean;
  /**
   * Callback when the button is pressed.
   */
  onPress?: () => void;
}
