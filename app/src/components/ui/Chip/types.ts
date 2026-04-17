import type { ViewProps } from 'react-native';
import type { IconName } from '../../icons';

export type ChipSize = 'default' | 'small';

export interface ChipProps extends Omit<ViewProps, 'children'> {
  /**
   * Chip label text. If not provided, chip will be icon-only.
   */
  label?: string;
  /**
   * Icon name to display on the left side of the chip.
   */
  icon?: IconName;
  /**
   * Chip size variant.
   * @default 'default'
   */
  size?: ChipSize;
  /**
   * Whether the chip is disabled.
   */
  disabled?: boolean;
}
