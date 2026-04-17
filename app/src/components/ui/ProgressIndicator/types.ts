import type { ViewStyle } from 'react-native';
import type { IconName } from '../../icons';

export interface ProgressStep {
  /**
   * Unique identifier for the step.
   */
  id: string;
  /**
   * Label displayed below the step indicator.
   */
  label: string;
  /**
   * Icon to display in the step indicator.
   * @default 'plus'
   */
  icon?: IconName;
}

export interface ProgressIndicatorProps {
  /**
   * Array of steps to display.
   */
  steps: ProgressStep[];
  /**
   * Index of the currently active step (0-based).
   */
  activeIndex?: number;
  /**
   * Callback when a step is pressed.
   */
  onStepPress?: (index: number, step: ProgressStep) => void;
  /**
   * Custom style for the container.
   */
  style?: ViewStyle;
}
