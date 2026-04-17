import type { ViewProps } from 'react-native';
import type { IconName } from '../../icons';

export interface ToastProps extends ViewProps {
  /**
   * The title text to display in the toast.
   */
  title: string;
  /**
   * Optional icon to display on the left side.
   * @default 'plus'
   */
  icon?: IconName;
}
