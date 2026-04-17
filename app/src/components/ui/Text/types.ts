import type { TextProps as RNTextProps } from 'react-native';
import type { typography } from '../../../theme';

type Variant = keyof typeof typography;
type Weight = 'regular' | 'semiBold';

export interface TextProps extends RNTextProps {
  /**
   * Typography variant to use.
   * @default 'body'
   */
  variant?: Variant;
  /**
   * Font weight.
   * @default 'regular'
   */
  weight?: Weight;
  /**
   * Custom text color. If not provided, uses theme's primary label color.
   */
  color?: string;
}
