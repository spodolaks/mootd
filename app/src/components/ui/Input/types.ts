import type { TextInputProps } from 'react-native';

export interface InputProps extends TextInputProps {
  /** Title label displayed above the input */
  title?: string;
  /** Description text displayed below the title */
  description?: string;
  /** Error message - displays error state when provided */
  error?: string;
  /** Disabled state */
  disabled?: boolean;
  /** Left icon element */
  leftIcon?: React.ReactNode;
  /** Right icon element */
  rightIcon?: React.ReactNode;
  /** Callback when left icon is pressed */
  onLeftIconPress?: () => void;
  /** Callback when right icon is pressed */
  onRightIconPress?: () => void;
}
