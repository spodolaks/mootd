import type { ViewProps } from 'react-native';

export interface InfoProps extends ViewProps {
  /**
   * The title text to display.
   */
  title: string;
  /**
   * Optional description text to display below the title.
   */
  description?: string;
  /**
   * Callback when the close button is pressed.
   * If not provided, the close button will not be shown.
   */
  onClose?: () => void;
}
