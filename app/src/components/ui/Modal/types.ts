import type { ModalProps as RNModalProps, ViewStyle } from 'react-native';

export interface ModalProps extends Omit<RNModalProps, 'children'> {
  /**
   * Title label displayed above the description
   */
  title?: string;
  /**
   * Description text displayed below the title
   */
  description?: string;
  /**
   * Primary button label
   */
  buttonLabel?: string;
  /**
   * Callback when primary button is pressed
   */
  onButtonPress?: () => void;
  /**
   * Callback when modal is dismissed (tapping outside or grabber)
   */
  onDismiss?: () => void;
  /**
   * Custom content to render in the modal
   */
  children?: React.ReactNode;
  /**
   * Whether to show the grabber handle
   * @default true
   */
  showGrabber?: boolean;
  /**
   * Custom style for the content container
   */
  contentStyle?: ViewStyle;
}
