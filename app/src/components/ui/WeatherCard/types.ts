import type { ViewStyle } from 'react-native';
import type { IconName } from '../../icons/Icon';

export interface WeatherCardProps {
  /**
   * Icon name for the weather condition. Defaults to 'cloud'.
   */
  icon?: IconName;
  /**
   * Current temperature value (number only, degree symbol added automatically)
   */
  temperature: number;
  /**
   * Temperature unit to display
   * @default 'c'
   */
  unit?: 'c' | 'f';
  /**
   * Weather condition text (e.g., "Cloudy", "Sunny", "Rainy")
   */
  condition: string;
  /**
   * Low temperature for the day
   */
  lowTemperature: number;
  /**
   * High temperature for the day
   */
  highTemperature: number;
  /**
   * Location name to display
   */
  location: string;
  /**
   * Callback when location dropdown is pressed
   */
  onLocationPress?: () => void;
  /**
   * Optional style for the container
   */
  style?: ViewStyle;
}
