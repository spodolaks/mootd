import { ViewStyle, ImageSourcePropType } from 'react-native';

export interface ClothingItemCardProps {
  /**
   * Label displayed below the image
   */
  label: string;
  /**
   * Whether the card is selected
   */
  selected?: boolean;
  /**
   * Image source for the clothing item
   */
  imageSource?: ImageSourcePropType;
  /**
   * Callback when the card is pressed
   */
  onPress?: () => void;
  /**
   * Whether the card is disabled
   */
  disabled?: boolean;
  /**
   * When true, renders image with contain mode on a dark grey background (for PNG with transparency)
   */
  darkBackground?: boolean;
  /**
   * Additional styles for the container
   */
  style?: ViewStyle;
}
